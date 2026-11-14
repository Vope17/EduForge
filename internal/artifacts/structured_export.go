package artifacts

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"

	"eduforge/internal/documents"
	"eduforge/internal/domain"
)

// Exports of the structured outputs: C lesson plan, D worksheet (a student
// copy and a teacher copy), S syllabus. Word files are built here (the plan is
// written into a form, see plan_form.go); PDFs are the Word file converted by
// LibreOffice, so the two always agree; HTML is the editor's preview.

const (
	mimeDocx = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	mimePDF  = "application/pdf"
	mimeHTML = "text/html; charset=utf-8"
	mimeTSV  = "text/tab-separated-values; charset=utf-8"
)

// ErrExportFormat is returned for a format the output does not have.
var ErrExportFormat = errors.New("unsupported_export_format")

// StructuredExport writes output a (type C, D or S) in format: docx, pdf,
// html or, for a syllabus, tsv. version "teacher" selects a worksheet's
// teacher copy. form is the Word form a plan is written into (nil for the
// built-in one). It returns the file, its media type and a file name.
func StructuredExport(ctx context.Context, a domain.Artifact, format, version string, form []byte) ([]byte, string, string, error) {
	teacher := version == "teacher"
	base := "EduForge-" + map[string]string{"C": "lesson-plan", "D": "worksheet", "S": "syllabus"}[a.Type] + "-" + a.Language
	if a.Type == "D" && teacher {
		base += "-teacher"
	}
	var docx []byte
	build := func() error {
		var err error
		switch {
		case a.Type == "C" && a.Plan != nil:
			if form == nil {
				form = PlanFormTemplate(a.Language)
			}
			docx, _, err = FillPlanForm(form, *a.Plan, a.Language)
		case a.Type == "D" && a.Worksheet != nil:
			docx = worksheetDocx(*a.Worksheet, a.Language, teacher)
		case a.Type == "S" && a.Syllabus != nil:
			docx = syllabusDocx(*a.Syllabus, a.Language)
		default:
			err = errors.New("output_empty")
		}
		return err
	}
	switch format {
	case "html":
		b, err := structuredHTML(a, teacher)
		return b, mimeHTML, base + ".html", err
	case "tsv":
		if a.Type != "S" || a.Syllabus == nil {
			return nil, "", "", ErrExportFormat
		}
		return []byte(syllabusTSV(*a.Syllabus, a.Language)), mimeTSV, base + ".tsv", nil
	case "docx":
		if err := build(); err != nil {
			return nil, "", "", err
		}
		return docx, mimeDocx, base + ".docx", nil
	case "pdf":
		if err := build(); err != nil {
			return nil, "", "", err
		}
		pdf, err := docxToPDF(ctx, docx)
		return pdf, mimePDF, base + ".pdf", err
	}
	return nil, "", "", ErrExportFormat
}

func docxToPDF(ctx context.Context, docx []byte) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "eduforge-docx-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	file := filepath.Join(tmp, "output.docx")
	if err = os.WriteFile(file, docx, 0o600); err != nil {
		return nil, err
	}
	pdf, err := documents.ConvertPDF(ctx, file, tmp)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(pdf)
}

func pick(lang, zh, en string) string {
	if lang == "en" {
		return en
	}
	return zh
}

// ---- Worksheet ---------------------------------------------------------

func choiceLetter(i int) string { return string(rune('A' + i)) }

func worksheetDocx(w domain.Worksheet, lang string, teacher bool) []byte {
	l := func(zh, en string) string { return pick(lang, zh, en) }
	total := 0
	for _, q := range w.Questions {
		total += q.Points
	}
	body := []*xnode{wPara("center", 80, wRun{Text: w.Title, Bold: true, Size: 34})}
	if teacher {
		body = append(body, wPara("center", 120, wRun{Text: l("教師版（含答案與解析）", "Teacher copy (with answers and explanations)"), Bold: true, Size: 22, Color: "A14A1B"}))
	} else {
		body = append(body, wPara("", 160, wRun{Text: l("姓名：____________　班級：________　座號：______　日期：____________", "Name: ____________   Class: ________   No.: ______   Date: ____________"), Size: 21}))
	}
	body = append(body, wPara("right", 160, wRun{Text: fmt.Sprintf(l("共 %d 題，滿分 %d 分", "%d questions, %d points"), len(w.Questions), total), Size: 20, Color: "666666"}))
	for i, q := range w.Questions {
		body = append(body, wPara("", 60, wRun{Text: fmt.Sprintf("%d. ", i+1), Bold: true, Size: 22}, wRun{Text: fmt.Sprintf(l("（%d 分）", "(%d pt) "), q.Points), Size: 20, Color: "666666"}, wRun{Text: q.Prompt, Size: 22}))
		for j, c := range q.Choices {
			body = append(body, wPara("", 20, wRun{Text: "　（" + choiceLetter(j) + "）" + c, Size: 22}))
		}
		if !teacher && q.Type != "choice" && q.Type != "fill" {
			lines := map[string]int{"short": 2, "calc": 5, "practice": 5}[q.Type]
			for n := 0; n < lines; n++ {
				body = append(body, wPara("", 40, wRun{Text: "　______________________________________________________________", Size: 22, Color: "999999"}))
			}
		}
		if teacher {
			body = append(body, wPara("", 20, wRun{Text: "　" + l("答案：", "Answer: "), Bold: true, Size: 21, Color: "0F5F52"}, wRun{Text: q.Answer, Size: 21, Color: "0F5F52"}))
			if strings.TrimSpace(q.Explanation) != "" {
				body = append(body, wPara("", 20, wRun{Text: "　" + l("解析：", "Explanation: "), Bold: true, Size: 21, Color: "0F5F52"}, wRun{Text: q.Explanation, Size: 21, Color: "0F5F52"}))
			}
			body = append(body, wPara("", 40, wRun{Text: "　" + l("對應目標：", "Objective: ") + q.Objective, Size: 18, Color: "888888"}))
		}
		body = append(body, wPara("", 80, wRun{Text: "", Size: 8}))
	}
	return docxBytes(body...)
}

// ---- Syllabus ----------------------------------------------------------

func syllabusDocx(s domain.Syllabus, lang string) []byte {
	l := func(zh, en string) string { return pick(lang, zh, en) }
	const shade = "E8F0EC"
	head := func(w int, text string) *xnode { return wCell(w, 1, shade, true, text) }
	cell := func(w int, text string) *xnode { return wCell(w, 1, "", false, text) }
	body := []*xnode{
		wPara("center", 60, wRun{Text: s.Name, Bold: true, Size: 36}),
		wPara("center", 160, wRun{Text: l("教學大綱與進度表", "Syllabus and schedule"), Size: 24, Color: "666666"}),
		wTable([]int{1800, 3019, 1800, 3019},
			wRow(head(1800, l("課程名稱", "Course")), cell(3019, s.Name), head(1800, l("授課對象", "Audience")), cell(3019, s.Audience)),
			wRow(head(1800, l("週數", "Weeks")), cell(3019, fmt.Sprint(s.Weeks)), head(1800, l("教學方式", "Method")), cell(3019, "")),
		),
		wPara("", 100, wRun{Text: "", Size: 8}),
		wPara("", 60, wRun{Text: l("課程描述", "Description"), Bold: true, Size: 26}),
	}
	for _, line := range lines(s.Description) {
		body = append(body, wPara("", 60, wRun{Text: line, Size: 21}))
	}
	body = append(body, wPara("", 60, wRun{Text: l("課程目標", "Objectives"), Bold: true, Size: 26}))
	for i, o := range s.Objectives {
		body = append(body, wPara("", 40, wRun{Text: fmt.Sprintf("%d. %s", i+1, o), Size: 21}))
	}
	body = append(body, wPara("", 100, wRun{Text: l("每週進度", "Weekly schedule"), Bold: true, Size: 26}))
	w := []int{800, 2600, 2300, 1700, 2238}
	rows := []*xnode{wRow(head(w[0], l("週次", "Week")), head(w[1], l("主題", "Topic")), head(w[2], l("對應章節", "Chapters")), head(w[3], l("教學方式", "Method")), head(w[4], l("作業與評量", "Homework and assessment")))}
	for _, r := range s.Schedule {
		rows = append(rows, wRow(cell(w[0], fmt.Sprint(r.Week)), cell(w[1], r.Topic), cell(w[2], r.Chapters), cell(w[3], r.Method), cell(w[4], r.Assessment)))
	}
	body = append(body, wTable(w, rows...), wPara("", 100, wRun{Text: "", Size: 8}), wPara("", 60, wRun{Text: l("評分方式", "Grading"), Bold: true, Size: 26}))
	grades := []*xnode{wRow(head(6000, l("項目", "Item")), head(3638, l("比例（%）", "Percent")))}
	for _, g := range s.Grading {
		grades = append(grades, wRow(cell(6000, g.Item), cell(3638, fmt.Sprint(g.Percent))))
	}
	body = append(body, wTable([]int{6000, 3638}, grades...), wPara("", 100, wRun{Text: "", Size: 8}), wPara("", 60, wRun{Text: l("教科書與參考書目", "Textbooks and references"), Bold: true, Size: 26}))
	for i, b := range s.Books {
		body = append(body, wPara("", 40, wRun{Text: fmt.Sprintf("%d. %s", i+1, b), Size: 21}))
	}
	return docxBytes(body...)
}

// syllabusTSV is the weekly schedule as tab-separated text, one row a week,
// for pasting into a school's system.
func syllabusTSV(s domain.Syllabus, lang string) string {
	clean := func(t string) string {
		return strings.Join(strings.Fields(strings.NewReplacer("\t", " ", "\n", " ").Replace(t)), " ")
	}
	var b strings.Builder
	b.WriteString(strings.Join([]string{pick(lang, "週次", "Week"), pick(lang, "主題", "Topic"), pick(lang, "對應章節", "Chapters"), pick(lang, "教學方式", "Method"), pick(lang, "作業與評量", "Homework and assessment")}, "\t") + "\n")
	for _, r := range s.Schedule {
		b.WriteString(strings.Join([]string{fmt.Sprint(r.Week), clean(r.Topic), clean(r.Chapters), clean(r.Method), clean(r.Assessment)}, "\t") + "\n")
	}
	return b.String()
}

// ---- HTML preview ------------------------------------------------------

const structuredCSS = `body{font-family:"Noto Sans CJK TC","Noto Sans TC",system-ui,sans-serif;color:#19332f;margin:32px auto;max-width:860px;padding:0 20px;line-height:1.6}h1{text-align:center;font-size:26px}h2{font-size:18px;margin-top:28px;border-bottom:1px solid #cfd8cc;padding-bottom:4px}table{border-collapse:collapse;width:100%;margin:10px 0}td,th{border:1px solid #9aa8a0;padding:6px 8px;vertical-align:top;font-size:14px}th,td.label{background:#e8f0ec;text-align:left;font-weight:600}.muted{color:#666;font-size:13px}.answer{color:#0f5f52;margin:2px 0 2px 1.5em}.q{margin:14px 0 4px}.choices{margin:2px 0 2px 1.5em}.blank{color:#aaa}`

func htmlRow(cells ...string) string {
	return "<tr>" + strings.Join(cells, "") + "</tr>"
}

func esc(s string) string { return strings.ReplaceAll(html.EscapeString(s), "\n", "<br>") }

func structuredHTML(a domain.Artifact, teacher bool) ([]byte, error) {
	l := func(zh, en string) string { return pick(a.Language, zh, en) }
	var b strings.Builder
	title := ""
	switch {
	case a.Type == "C" && a.Plan != nil:
		p := a.Plan
		title = l("單元教案", "Lesson plan")
		fmt.Fprintf(&b, "<h1>%s</h1><table>", esc(title))
		b.WriteString(htmlRow(`<td class="label">`+l("單元名稱", "Unit name")+"</td>", "<td>"+esc(p.Info.Unit)+"</td>", `<td class="label">`+l("設計者", "Designer")+"</td>", "<td>"+esc(p.Info.Designer)+"</td>"))
		b.WriteString(htmlRow(`<td class="label">`+l("教學對象", "Audience")+"</td>", "<td>"+esc(p.Info.Audience)+"</td>", `<td class="label">`+l("教學時間", "Class time")+"</td>", "<td>"+esc(fmt.Sprintf(l("%d 分鐘", "%d minutes"), p.Info.Minutes))+"</td>"))
		b.WriteString(htmlRow(`<td class="label">` + l("教材來源", "Source") + `</td><td colspan="3">` + esc(p.Info.Source) + "</td>"))
		b.WriteString("</table><table>")
		for _, r := range [][2]string{{l("教材分析", "Material analysis"), p.Analysis}, {l("學生經驗", "Student experience"), p.Experience}, {l("單元目標", "Unit goal"), p.Goal}} {
			b.WriteString(htmlRow(`<td class="label" style="width:20%">`+esc(r[0])+"</td>", "<td>"+esc(r[1])+"</td>"))
		}
		for _, d := range [][2]string{{"cognitive", l("認知目標", "Cognitive")}, {"affective", l("情意目標", "Affective")}, {"skill", l("技能目標", "Skill")}} {
			b.WriteString(htmlRow(`<td class="label">`+esc(d[1])+"</td>", "<td>"+esc(strings.Join(objectiveLines(*p, d[0]), "\n"))+"</td>"))
		}
		b.WriteString("</table><table>")
		b.WriteString(htmlRow("<th>"+l("階段", "Phase")+"</th>", "<th>"+l("目標代號", "Objectives")+"</th>", "<th>"+l("教學活動", "Activity")+"</th>", "<th>"+l("時間（分鐘）", "Minutes")+"</th>", "<th>"+l("教學資源", "Resources")+"</th>", "<th>"+l("評量方式", "Assessment")+"</th>"))
		last, sum := "", 0
		for _, act := range p.Activities {
			phase := ""
			if act.Phase != last {
				phase = phaseLabel(act.Phase, a.Language)
			}
			last, sum = act.Phase, sum+act.Minutes
			b.WriteString(htmlRow("<td>"+esc(phase)+"</td>", "<td>"+esc(strings.Join(act.Objectives, ", "))+"</td>", "<td><strong>"+esc(act.Title)+"</strong><br>"+esc(act.Description)+"</td>", "<td>"+fmt.Sprint(act.Minutes)+"</td>", "<td>"+esc(act.Resources)+"</td>", "<td>"+esc(act.Assessment)+"</td>"))
		}
		b.WriteString(htmlRow(`<td colspan="3" class="label">`+l("合計", "Total")+"</td>", "<td>"+fmt.Sprint(sum)+`</td><td colspan="2"></td>`))
		b.WriteString("</table><table>" + htmlRow(`<td class="label" style="width:20%">`+l("參考資料", "References")+"</td>", "<td>"+esc(strings.Join(p.References, "\n"))+"</td>") + "</table>")
	case a.Type == "D" && a.Worksheet != nil:
		w := a.Worksheet
		title = w.Title
		fmt.Fprintf(&b, "<h1>%s</h1>", esc(w.Title))
		if teacher {
			fmt.Fprintf(&b, `<p class="muted">%s</p>`, l("教師版（含答案與解析）", "Teacher copy (with answers and explanations)"))
		} else {
			fmt.Fprintf(&b, `<p class="muted">%s</p>`, l("姓名：________　班級：______　座號：____", "Name: ________   Class: ______   No.: ____"))
		}
		for i, q := range w.Questions {
			fmt.Fprintf(&b, `<p class="q"><strong>%d.</strong> <span class="muted">(%d)</span> %s</p>`, i+1, q.Points, esc(q.Prompt))
			for j, c := range q.Choices {
				fmt.Fprintf(&b, `<div class="choices">（%s）%s</div>`, choiceLetter(j), esc(c))
			}
			if !teacher && q.Type != "choice" && q.Type != "fill" {
				b.WriteString(`<div class="blank">__________________________________________</div>`)
			}
			if teacher {
				fmt.Fprintf(&b, `<div class="answer"><strong>%s</strong>%s</div>`, l("答案：", "Answer: "), esc(q.Answer))
				if q.Explanation != "" {
					fmt.Fprintf(&b, `<div class="answer"><strong>%s</strong>%s</div>`, l("解析：", "Explanation: "), esc(q.Explanation))
				}
			}
		}
	case a.Type == "S" && a.Syllabus != nil:
		s := a.Syllabus
		title = s.Name
		fmt.Fprintf(&b, "<h1>%s</h1><p class=\"muted\" style=\"text-align:center\">%s</p><h2>%s</h2><p>%s</p><h2>%s</h2><ol>", esc(s.Name), l("教學大綱與進度表", "Syllabus and schedule"), l("課程描述", "Description"), esc(s.Description), l("課程目標", "Objectives"))
		for _, o := range s.Objectives {
			fmt.Fprintf(&b, "<li>%s</li>", esc(o))
		}
		fmt.Fprintf(&b, "</ol><h2>%s</h2><table>", l("每週進度", "Weekly schedule"))
		b.WriteString(htmlRow("<th>"+l("週次", "Week")+"</th>", "<th>"+l("主題", "Topic")+"</th>", "<th>"+l("對應章節", "Chapters")+"</th>", "<th>"+l("教學方式", "Method")+"</th>", "<th>"+l("作業與評量", "Homework and assessment")+"</th>"))
		for _, r := range s.Schedule {
			b.WriteString(htmlRow("<td>"+fmt.Sprint(r.Week)+"</td>", "<td>"+esc(r.Topic)+"</td>", "<td>"+esc(r.Chapters)+"</td>", "<td>"+esc(r.Method)+"</td>", "<td>"+esc(r.Assessment)+"</td>"))
		}
		fmt.Fprintf(&b, "</table><h2>%s</h2><table>", l("評分方式", "Grading"))
		for _, g := range s.Grading {
			b.WriteString(htmlRow("<td>"+esc(g.Item)+"</td>", fmt.Sprintf("<td>%d%%</td>", g.Percent)))
		}
		fmt.Fprintf(&b, "</table><h2>%s</h2><ol>", l("教科書與參考書目", "Textbooks and references"))
		for _, bk := range s.Books {
			fmt.Fprintf(&b, "<li>%s</li>", esc(bk))
		}
		b.WriteString("</ol>")
	default:
		return nil, errors.New("output_empty")
	}
	return []byte(`<!doctype html><html lang="` + a.Language + `"><head><meta charset="utf-8"><title>` + html.EscapeString(title) + `</title><style>` + structuredCSS + `</style></head><body>` + b.String() + `</body></html>`), nil
}
