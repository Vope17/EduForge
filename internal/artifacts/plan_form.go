package artifacts

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"eduforge/internal/domain"
)

// A lesson plan is written into a Word form. The form is any .docx whose
// tables carry the usual labels (單元名稱, 教學對象, 教學活動…): a label cell
// is followed by the cell its value goes into, and the activity table is the
// one whose header row names the activity columns. EduForge ships a plain
// form (PlanFormTemplate); a school's own form works the same way, because
// fields are found by label and not by position.

// planLabels lists, per field, the labels a form may use (compared after
// dropping spaces, punctuation and anything in parentheses).
var planLabels = map[string][]string{
	"unit":       {"單元名稱", "課程名稱", "教學單元", "單元", "unitname", "unit", "lesson"},
	"designer":   {"設計者", "設計人", "授課教師", "教學者", "教師", "designer", "teacher"},
	"audience":   {"教學對象", "適用對象", "對象", "audience", "students"},
	"minutes":    {"教學時間", "授課時間", "教學節數", "duration", "classtime"},
	"source":     {"教材來源", "課程來源", "教材", "source", "material"},
	"analysis":   {"教材分析", "analysis", "materialanalysis"},
	"experience": {"學生經驗", "學生特性", "學生起點行為", "學生分析", "studentexperience", "studentbackground"},
	"goal":       {"單元目標", "教學目標", "單元教學目標", "goal", "unitgoal"},
	"cognitive":  {"認知目標", "認知", "cognitive"},
	"affective":  {"情意目標", "情意", "affective"},
	"skill":      {"技能目標", "技能", "skill"},
	"references": {"參考資料", "參考文獻", "references"},
}

// planColumns lists the labels of the activity table's columns.
var planColumns = map[string][]string{
	"phase":      {"階段", "教學階段", "phase", "stage"},
	"objectives": {"目標代號", "具體目標", "對應目標", "目標", "objectives", "code"},
	"activity":   {"教學活動", "教學活動內容", "活動", "activity", "activities"},
	"minutes":    {"時間", "教學時間", "分鐘", "time", "minutes"},
	"resources":  {"教學資源", "教具", "資源", "resources"},
	"assessment": {"評量方式", "評量", "assessment"},
}

var totalLabels = []string{"合計", "總計", "total"}

// normalizeLabel drops parentheses and what is inside, spaces, punctuation and case.
func normalizeLabel(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '(' || r == '（' || r == '[' || r == '【':
			depth++
		case r == ')' || r == '）' || r == ']' || r == '】':
			if depth > 0 {
				depth--
			}
		case depth == 0 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func fieldOf(labels map[string][]string, text string) string {
	n := normalizeLabel(text)
	if n == "" {
		return ""
	}
	for field, names := range labels {
		if slices.Contains(names, n) {
			return field
		}
	}
	return ""
}

// gridRow is a table row with its cells placed on grid columns.
type gridRow struct {
	row   *xnode
	cells []*xnode
	col   []int // grid column each cell starts at
}

func newGridRow(row *xnode) gridRow {
	g := gridRow{row: row, cells: row.children("w:tc")}
	at := 0
	for _, tc := range g.cells {
		g.col = append(g.col, at)
		span := 1
		if pr := tc.child("w:tcPr"); pr != nil {
			if gs := pr.child("w:gridSpan"); gs != nil {
				for _, a := range gs.attrs {
					if a.Name.Local == "val" {
						if n, err := strconv.Atoi(a.Value); err == nil && n > 1 {
							span = n
						}
					}
				}
			}
		}
		at += span
	}
	return g
}

func (g gridRow) cellAt(col int) *xnode {
	for i, c := range g.col {
		if c == col {
			return g.cells[i]
		}
	}
	return nil
}

// setCellText replaces a cell's paragraphs with one paragraph per line,
// keeping the look (paragraph and run properties) of the cell's first paragraph.
func setCellText(tc *xnode, lines []string) {
	var ppr, rpr *xnode
	if p := tc.child("w:p"); p != nil {
		ppr = p.child("w:pPr")
		if r := p.child("w:r"); r != nil {
			rpr = r.child("w:rPr")
		}
	}
	kept := []*xnode{}
	for _, k := range tc.kids {
		if k.isText || k.raw != "" || k.name() == "w:tcPr" {
			kept = append(kept, k)
		}
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	for _, line := range lines {
		p := elem("w:p")
		if ppr != nil {
			p.kids = append(p.kids, ppr.clone())
		}
		run := elem("w:r")
		if rpr != nil {
			run.kids = append(run.kids, rpr.clone())
		}
		run.kids = append(run.kids, elem("w:t", textNode(line)).attr("xml:space", "preserve"))
		p.kids = append(p.kids, run)
		kept = append(kept, p)
	}
	tc.kids = kept
}

func phaseLabel(phase, lang string) string {
	zh := map[string]string{"prepare": "準備活動", "develop": "發展活動", "wrap": "綜合活動"}
	en := map[string]string{"prepare": "Preparation", "develop": "Development", "wrap": "Wrap-up"}
	if lang == "en" {
		return en[phase]
	}
	return zh[phase]
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}

func objectiveLines(p domain.LessonPlan, domainName string) []string {
	var out []string
	for _, o := range p.Objectives {
		if o.Domain == domainName {
			out = append(out, o.Code+" "+o.Text)
		}
	}
	if len(out) == 0 {
		return []string{"—"}
	}
	return out
}

func planValue(p domain.LessonPlan, field, lang string) []string {
	switch field {
	case "unit":
		return []string{p.Info.Unit}
	case "designer":
		return []string{p.Info.Designer}
	case "audience":
		return []string{p.Info.Audience}
	case "minutes":
		if lang == "en" {
			return []string{fmt.Sprintf("%d minutes", p.Info.Minutes)}
		}
		return []string{fmt.Sprintf("%d 分鐘", p.Info.Minutes)}
	case "source":
		return []string{p.Info.Source}
	case "analysis":
		return lines(p.Analysis)
	case "experience":
		return lines(p.Experience)
	case "goal":
		return lines(p.Goal)
	case "cognitive", "affective", "skill":
		return objectiveLines(p, field)
	case "references":
		return p.References
	}
	return nil
}

// activityRows fills clones of the form's data row, one per activity.
func fillActivities(tbl *xnode, rows []*xnode, header int, cols map[string]int, p domain.LessonPlan, lang string) {
	data := newGridRow(rows[header+1])
	total := -1
	for i := header + 2; i < len(rows); i++ {
		if first := rows[i].child("w:tc"); first != nil && slices.Contains(totalLabels, normalizeLabel(first.plainText())) {
			total = i
		}
	}
	var made []*xnode
	lastPhase := ""
	sum := 0
	for _, a := range p.Activities {
		row := data.row.clone()
		g := newGridRow(row)
		set := func(field string, text []string) {
			if col, ok := cols[field]; ok {
				if tc := g.cellAt(col); tc != nil {
					setCellText(tc, text)
				}
			}
		}
		activity := append([]string{a.Title}, lines(a.Description)...)
		if _, ok := cols["phase"]; ok {
			label := ""
			if a.Phase != lastPhase {
				label = phaseLabel(a.Phase, lang)
			}
			set("phase", []string{label})
		} else if a.Phase != lastPhase {
			activity = append([]string{"【" + phaseLabel(a.Phase, lang) + "】"}, activity...)
		}
		lastPhase = a.Phase
		set("objectives", []string{strings.Join(a.Objectives, ", ")})
		set("activity", activity)
		set("minutes", []string{strconv.Itoa(a.Minutes)})
		set("resources", lines(a.Resources))
		set("assessment", lines(a.Assessment))
		sum += a.Minutes
		made = append(made, row)
	}
	if len(made) == 0 {
		made = []*xnode{data.row}
	}
	// The header, the filled rows, then the total row and what follows it; the
	// template's own blank data rows are replaced.
	out := append([]*xnode{}, rows[:header+1]...)
	out = append(out, made...)
	if total >= 0 {
		out = append(out, rows[total:]...)
	}
	if total >= 0 {
		g := newGridRow(rows[total])
		if col, ok := cols["minutes"]; ok {
			if tc := g.cellAt(col); tc != nil {
				setCellText(tc, []string{strconv.Itoa(sum)})
			}
		}
	}
	// Keep everything that was not a row (table properties, the grid) in front.
	front := []*xnode{}
	for _, k := range tbl.kids {
		if k.isText || k.raw != "" || k.name() != "w:tr" {
			front = append(front, k)
		}
	}
	tbl.kids = append(front, out...)
}

// FillPlanForm writes the plan into a Word form and returns the new file and
// the labels the form needs but lacks (the unit name and the activity table).
func FillPlanForm(form []byte, p domain.LessonPlan, lang string) ([]byte, []string, error) {
	zr, err := zip.NewReader(bytes.NewReader(form), int64(len(form)))
	if err != nil {
		return nil, nil, errors.New("invalid_office_archive")
	}
	var doc *xnode
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return nil, nil, err
		}
		b, err := io.ReadAll(io.LimitReader(r, 64<<20))
		r.Close()
		if err != nil {
			return nil, nil, err
		}
		if doc, err = parseXML(b); err != nil {
			return nil, nil, errors.New("invalid_plan_form")
		}
	}
	if doc == nil {
		return nil, nil, errors.New("invalid_plan_form")
	}
	filled := map[string]bool{}
	activities := false
	for _, tbl := range doc.find("w:tbl") {
		rows := tbl.children("w:tr")
		// The activity table: a header row naming at least three activity columns.
		header, cols := -1, map[string]int{}
		for i, row := range rows {
			g := newGridRow(row)
			found := map[string]int{}
			for j, tc := range g.cells {
				if field := fieldOf(planColumns, tc.plainText()); field != "" {
					found[field] = g.col[j]
				}
			}
			if _, ok := found["activity"]; ok && len(found) >= 3 && i+1 < len(rows) {
				header, cols = i, found
				break
			}
		}
		if header >= 0 {
			fillActivities(tbl, rows, header, cols, p, lang)
			activities = true
			continue
		}
		for _, row := range rows {
			g := newGridRow(row)
			for j, tc := range g.cells {
				if field := fieldOf(planLabels, tc.plainText()); field != "" && j+1 < len(g.cells) {
					setCellText(g.cells[j+1], planValue(p, field, lang))
					filled[field] = true
				}
			}
		}
	}
	var missing []string
	if !filled["unit"] {
		missing = append(missing, "unit")
	}
	if !activities {
		missing = append(missing, "activities")
	}
	if len(missing) > 0 {
		return nil, missing, fmt.Errorf("plan_form_missing_labels: %s", strings.Join(missing, ","))
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		w, err := zw.Create(f.Name)
		if err != nil {
			return nil, nil, err
		}
		if f.Name == "word/document.xml" {
			_, err = w.Write(doc.bytes())
		} else {
			r, e := f.Open()
			if e != nil {
				return nil, nil, e
			}
			_, err = io.Copy(w, io.LimitReader(r, 256<<20))
			r.Close()
		}
		if err != nil {
			return nil, nil, err
		}
	}
	if err = zw.Close(); err != nil {
		return nil, nil, err
	}
	return out.Bytes(), nil, nil
}

// PlanFormTemplate is the form EduForge ships: the usual single-unit lesson
// plan tables, with their labels in lang.
func PlanFormTemplate(lang string) []byte {
	zh := lang != "en"
	l := func(zhText, enText string) string {
		if zh {
			return zhText
		}
		return enText
	}
	const shade = "E8F0EC"
	label := func(w int, text string) *xnode { return wCell(w, 1, shade, true, text) }
	value := func(w, span int) *xnode { return wCell(w, span, "", false, "") }
	spacer := func() *xnode { return wPara("", 60, wRun{Text: "", Size: 8}) }
	info := wTable([]int{1500, 3319, 1500, 3319},
		wRow(label(1500, l("單元名稱", "Unit name")), value(3319, 1), label(1500, l("設計者", "Designer")), value(3319, 1)),
		wRow(label(1500, l("教學對象", "Audience")), value(3319, 1), label(1500, l("教學時間", "Class time")), value(3319, 1)),
		wRow(label(1500, l("教材來源", "Source")), value(8138, 3)),
	)
	study := wTable([]int{1500, 8138},
		wRow(label(1500, l("教材分析", "Material analysis")), value(8138, 1)),
		wRow(label(1500, l("學生經驗", "Student experience")), value(8138, 1)),
	)
	goals := wTable([]int{1500, 8138},
		wRow(label(1500, l("單元目標", "Unit goal")), value(8138, 1)),
		wRow(label(1500, l("認知目標", "Cognitive")), value(8138, 1)),
		wRow(label(1500, l("情意目標", "Affective")), value(8138, 1)),
		wRow(label(1500, l("技能目標", "Skill")), value(8138, 1)),
	)
	w := []int{1100, 1000, 3338, 900, 1700, 1600}
	acts := wTable(w,
		wRow(label(w[0], l("階段", "Phase")), label(w[1], l("目標代號", "Objectives")), label(w[2], l("教學活動", "Activity")), label(w[3], l("時間（分鐘）", "Minutes")), label(w[4], l("教學資源", "Resources")), label(w[5], l("評量方式", "Assessment"))),
		wRow(value(w[0], 1), value(w[1], 1), value(w[2], 1), value(w[3], 1), value(w[4], 1), value(w[5], 1)),
		wRow(wCell(w[0]+w[1]+w[2], 3, shade, true, l("合計", "Total")), value(w[3], 1), wCell(w[4]+w[5], 2, "", false, "")),
	)
	refs := wTable([]int{1500, 8138}, wRow(label(1500, l("參考資料", "References")), value(8138, 1)))
	return docxBytes(
		wPara("center", 160, wRun{Text: l("單元教案", "Lesson plan"), Bold: true, Size: 36}),
		info, spacer(), study, spacer(), goals, spacer(), acts, spacer(), refs,
	)
}
