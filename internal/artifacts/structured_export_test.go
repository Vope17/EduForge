package artifacts

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"

	"eduforge/internal/domain"
)

func samplePlan() domain.LessonPlan {
	return domain.LessonPlan{
		Info:       domain.PlanInfo{Unit: "速率", Designer: "王老師", Audience: "大一學生", Minutes: 50, Source: "物理導論 / 第一章 / 速率"},
		Goal:       "學生能計算平均速率。",
		Analysis:   "本節定義速率。\n並以汽車為例。",
		Experience: "學生會除法。",
		Objectives: []domain.PlanObjective{
			{Code: "O1", Domain: "skill", Text: "計算平均速率"},
			{Code: "O2", Domain: "cognitive", Text: "說明速率是比率"},
			{Code: "O3", Domain: "affective", Text: "願意檢查單位"},
		},
		Activities: []domain.PlanActivity{
			{Phase: "prepare", Title: "暖身", Description: "詢問通勤時間。", Minutes: 10, Objectives: []string{"O2"}, Resources: "黑板", Assessment: "口頭提問"},
			{Phase: "develop", Title: "講解速率", Description: "定義並演算例題。", Minutes: 15, Objectives: []string{"O1", "O2"}, Resources: "簡報", Assessment: "課堂練習"},
			{Phase: "develop", Title: "分組練習", Description: "學生分組計算。", Minutes: 15, Objectives: []string{"O1", "O3"}, Resources: "學習單", Assessment: "學習單"},
			{Phase: "wrap", Title: "重點整理", Description: "回顧。", Minutes: 10, Objectives: []string{"O1"}, Resources: "講義", Assessment: "出場券"},
		},
		References: []string{"速率講義"},
	}
}

// docxText returns the text of a Word file's body, cells and paragraphs split by newlines.
func docxText(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			r, _ := f.Open()
			data, _ := io.ReadAll(r)
			r.Close()
			doc, err := parseXML(data)
			if err != nil {
				t.Fatal(err)
			}
			return doc.plainText()
		}
	}
	t.Fatal("no document.xml")
	return ""
}

func need(t *testing.T, tool string) {
	t.Helper()
	if _, err := exec.LookPath(tool); err != nil {
		t.Skipf("%s not installed", tool)
	}
}

func TestXMLTreeWritesBackWhatItRead(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="urn:x"><w:body><w:p w:rsidR="00A1"><w:r><w:t xml:space="preserve"> a &amp; b &lt;c&gt; </w:t></w:r><w:br/></w:p></w:body></w:document>`
	doc, err := parseXML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(doc.bytes()); got != src {
		t.Fatalf("round trip changed the document:\n%s\n%s", src, got)
	}
}

func TestPlanIsWrittenIntoTheBuiltInForm(t *testing.T) {
	form := PlanFormTemplate("zh-TW")
	out, missing, err := FillPlanForm(form, samplePlan(), "zh-TW")
	if err != nil || len(missing) != 0 {
		t.Fatalf("fill: %v %v", err, missing)
	}
	text := docxText(t, out)
	for _, want := range []string{"速率", "王老師", "大一學生", "50 分鐘", "物理導論 / 第一章 / 速率", "本節定義速率。\n並以汽車為例。", "O1 計算平均速率", "O2 說明速率是比率", "O3 願意檢查單位", "準備活動", "發展活動", "綜合活動", "講解速率", "定義並演算例題。", "分組練習", "O1, O3", "速率講義"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the form lacks %q:\n%s", want, text)
		}
	}
	// The labels stay, every activity has its own row, the phase shows once per phase, the total is the sum.
	if strings.Count(text, "發展活動") != 1 || !strings.Contains(text, "\n合計\n") && !strings.Contains(text, "合計") {
		t.Fatalf("phase labels:\n%s", text)
	}
	doc, _ := parseXML([]byte(unzipDocument(t, out)))
	var activities *xnode
	for _, tbl := range doc.find("w:tbl") {
		if strings.Contains(tbl.plainText(), "教學活動") {
			activities = tbl
		}
	}
	rows := activities.children("w:tr")
	if len(rows) != 1+4+1 {
		t.Fatalf("header + 4 activities + total, got %d rows", len(rows))
	}
	total := newGridRow(rows[len(rows)-1])
	if got := total.cellAt(3).plainText(); got != "50" {
		t.Fatalf("the total row sums the minutes: %q", got)
	}
	if !bytes.Contains(out, []byte("word/styles.xml")) {
		t.Fatal("the package keeps its other parts")
	}
	// English labels and phase names.
	enOut, _, err := FillPlanForm(PlanFormTemplate("en"), samplePlan(), "en")
	if err != nil {
		t.Fatal(err)
	}
	if et := docxText(t, enOut); !strings.Contains(et, "Preparation") || !strings.Contains(et, "50 minutes") || !strings.Contains(et, "Unit name") {
		t.Fatalf("english form:\n%s", et)
	}
}

func unzipDocument(t *testing.T, b []byte) string {
	t.Helper()
	zr, _ := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			r, _ := f.Open()
			data, _ := io.ReadAll(r)
			r.Close()
			return string(data)
		}
	}
	return ""
}

// schoolForm is a form with other labels, other order, no phase column and
// two blank rows: found by label, not by position.
func schoolForm() []byte {
	label := func(text string) *xnode { return wCell(1800, 1, "DDDDDD", true, text) }
	blank := func() *xnode { return wCell(3000, 1, "", false, "") }
	head := wTable([]int{1800, 3000, 1800, 3000},
		wRow(label("授課教師"), blank(), label("課程名稱"), blank()),
		wRow(label("適用對象"), blank(), label("授課時間"), blank()),
	)
	goals := wTable([]int{1800, 8000},
		wRow(label("教學目標"), wCell(8000, 1, "", false, "")),
		wRow(label("認知"), wCell(8000, 1, "", false, "")),
		wRow(label("情意"), wCell(8000, 1, "", false, "")),
		wRow(label("技能"), wCell(8000, 1, "", false, "")),
	)
	acts := wTable([]int{1500, 4500, 1200, 2000},
		wRow(label("具體目標"), label("教學活動內容"), label("時間（分）"), label("評量")),
		wRow(blank(), blank(), blank(), blank()),
		wRow(blank(), blank(), blank(), blank()),
		wRow(wCell(6000, 2, "", true, "總計"), wCell(1200, 1, "", false, ""), wCell(2000, 1, "", false, "")),
	)
	return docxBytes(head, wPara("", 60), goals, wPara("", 60), acts)
}

func TestPlanIsWrittenIntoASchoolFormByLabel(t *testing.T) {
	out, missing, err := FillPlanForm(schoolForm(), samplePlan(), "zh-TW")
	if err != nil || len(missing) != 0 {
		t.Fatalf("a school form with other labels is filled: %v %v", err, missing)
	}
	text := docxText(t, out)
	for _, want := range []string{"王老師", "速率", "大一學生", "50 分鐘", "學生能計算平均速率。", "O1 計算平均速率", "O3 願意檢查單位", "【準備活動】", "【發展活動】", "【綜合活動】", "分組練習"} {
		if !strings.Contains(text, want) {
			t.Fatalf("lacks %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "【發展活動】") != 1 {
		t.Fatal("without a phase column the phase label heads its first activity")
	}
	doc, _ := parseXML([]byte(unzipDocument(t, out)))
	tbls := doc.find("w:tbl")
	rows := tbls[len(tbls)-1].children("w:tr")
	if len(rows) != 1+4+1 || newGridRow(rows[len(rows)-1]).cellAt(2).plainText() != "50" {
		t.Fatalf("the template's two blank rows become the four activities, and the total is filled: %d rows", len(rows))
	}
}

func TestFormWithoutTheNeededLabelsIsRefused(t *testing.T) {
	plain := docxBytes(wPara("", 60, wRun{Text: "Nothing here"}))
	_, missing, err := FillPlanForm(plain, samplePlan(), "zh-TW")
	if err == nil || strings.Join(missing, ",") != "unit,activities" || !strings.Contains(err.Error(), "plan_form_missing_labels") {
		t.Fatalf("missing = %v, err = %v", missing, err)
	}
	if _, _, err = FillPlanForm([]byte("not a zip"), samplePlan(), "zh-TW"); err == nil {
		t.Fatal("not a Word file")
	}
	onlyUnit := docxBytes(wTable([]int{1800, 3000}, wRow(wCell(1800, 1, "", true, "單元名稱"), wCell(3000, 1, "", false, ""))))
	if _, missing, _ = FillPlanForm(onlyUnit, samplePlan(), "zh-TW"); strings.Join(missing, ",") != "activities" {
		t.Fatalf("missing = %v", missing)
	}
}

func sampleSheet() domain.Worksheet {
	return domain.Worksheet{Title: "速率學習單", Questions: []domain.Question{
		{Type: "choice", Prompt: "速率的定義是？", Choices: []string{"距離除以時間", "時間除以距離", "距離乘以時間"}, Answer: "A", Explanation: "速率是單位時間的距離。", Objective: "O1", Points: 2},
		{Type: "fill", Prompt: "速率 = ____ ÷ 時間", Answer: "距離", Explanation: "定義", Objective: "O1", Points: 1},
		{Type: "calc", Prompt: "汽車 2 小時行駛 120 公里，速率是多少？", Answer: "60 公里／小時", Explanation: "120 ÷ 2 = 60", Objective: "O2", Points: 3},
	}}
}

func TestWorksheetStudentCopyNeverContainsAnswers(t *testing.T) {
	a := domain.Artifact{Type: "D", Language: "zh-TW", Worksheet: ptr(sampleWorksheet())}
	student, _, _, err := StructuredExport(context.Background(), a, "docx", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	teacher, _, _, err := StructuredExport(context.Background(), a, "docx", "teacher", nil)
	if err != nil {
		t.Fatal(err)
	}
	st, tt := docxText(t, student), docxText(t, teacher)
	for _, secret := range []string{"60 公里／小時", "120 ÷ 2 = 60", "速率是單位時間的距離。", "答案", "解析", "教師版"} {
		if strings.Contains(st, secret) {
			t.Fatalf("the student copy leaks %q:\n%s", secret, st)
		}
	}
	for _, want := range []string{"速率的定義是？", "（A）距離除以時間", "姓名", "共 3 題，滿分 6 分"} {
		if !strings.Contains(st, want) {
			t.Fatalf("the student copy lacks %q:\n%s", want, st)
		}
	}
	for _, want := range []string{"教師版", "答案：A", "答案：60 公里／小時", "解析：120 ÷ 2 = 60", "對應目標：O2"} {
		if !strings.Contains(tt, want) {
			t.Fatalf("the teacher copy lacks %q:\n%s", want, tt)
		}
	}
	// The HTML preview follows the same rule.
	html, _, _, _ := StructuredExport(context.Background(), a, "html", "", nil)
	if strings.Contains(string(html), "60 公里") || strings.Contains(string(html), "答案") {
		t.Fatal("the student preview leaks answers")
	}
	teacherHTML, _, name, _ := StructuredExport(context.Background(), a, "html", "teacher", nil)
	if !strings.Contains(string(teacherHTML), "60 公里") || !strings.HasSuffix(name, "worksheet-zh-TW-teacher.html") {
		t.Fatalf("the teacher preview shows answers: %s", name)
	}
}

func sampleWorksheet() domain.Worksheet { return sampleSheet() }
func ptr[T any](v T) *T                 { return &v }

func TestSyllabusExportsWordAndTabSeparatedText(t *testing.T) {
	s := domain.Syllabus{Name: "物理導論", Audience: "大一", Weeks: 2, Description: "力學入門。", Objectives: []string{"理解運動"},
		Schedule: []domain.SyllabusWeek{{Week: 1, Topic: "速率", Chapters: "第一章", Method: "講述", Assessment: "作業 1"}, {Week: 2, Topic: "力\t與運動", Chapters: "第二章", Method: "實驗", Assessment: ""}},
		Grading:  []domain.GradeItem{{Item: "期末考", Percent: 60}, {Item: "作業", Percent: 40}}, Books: []string{"普通物理"}}
	a := domain.Artifact{Type: "S", Language: "zh-TW", Syllabus: &s}
	docx, _, _, err := StructuredExport(context.Background(), a, "docx", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	text := docxText(t, docx)
	for _, want := range []string{"物理導論", "教學大綱與進度表", "週次", "速率", "作業 1", "期末考", "60", "普通物理", "力學入門。"} {
		if !strings.Contains(text, want) {
			t.Fatalf("syllabus lacks %q:\n%s", want, text)
		}
	}
	tsv, mime, name, err := StructuredExport(context.Background(), a, "tsv", "", nil)
	if err != nil || !strings.HasPrefix(mime, "text/tab-separated-values") || !strings.HasSuffix(name, ".tsv") {
		t.Fatalf("tsv: %v %s %s", err, mime, name)
	}
	rows := strings.Split(strings.TrimSuffix(string(tsv), "\n"), "\n")
	if len(rows) != 3 || strings.Count(rows[0], "\t") != 4 || strings.Count(rows[2], "\t") != 4 || !strings.Contains(rows[2], "力 與運動") {
		t.Fatalf("one row a week, five columns, tabs inside fields removed:\n%s", tsv)
	}
	if _, _, _, err = StructuredExport(context.Background(), domain.Artifact{Type: "D", Worksheet: &domain.Worksheet{}}, "tsv", "", nil); err == nil {
		t.Fatal("only a syllabus has tsv")
	}
	if _, _, _, err = StructuredExport(context.Background(), domain.Artifact{Type: "C"}, "docx", "", nil); err == nil {
		t.Fatal("an empty plan cannot be exported")
	}
}

func TestStructuredPDFsComeFromTheWordFiles(t *testing.T) {
	need(t, "libreoffice")
	need(t, "pdftotext")
	plan := domain.Artifact{Type: "C", Language: "zh-TW", Plan: ptr(samplePlan())}
	pdf, mime, name, err := StructuredExport(context.Background(), plan, "pdf", "", nil)
	if err != nil || mime != "application/pdf" || !bytes.HasPrefix(pdf, []byte("%PDF-")) || !strings.HasSuffix(name, ".pdf") {
		t.Fatalf("plan pdf: %v %s", err, name)
	}
	cmd := exec.Command("pdftotext", "-", "-")
	cmd.Stdin = bytes.NewReader(pdf)
	out, err := cmd.Output()
	if err != nil || !strings.Contains(string(out), "分組練習") || !strings.Contains(string(out), "單元教案") {
		t.Fatalf("the plan's text is in the PDF: %v\n%s", err, out)
	}
	sheet := domain.Artifact{Type: "D", Language: "zh-TW", Worksheet: ptr(sampleSheet())}
	student, _, _, err := StructuredExport(context.Background(), sheet, "pdf", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("pdftotext", "-", "-")
	cmd.Stdin = bytes.NewReader(student)
	out, _ = cmd.Output()
	if strings.Contains(string(out), "60 公里／小時") || !strings.Contains(string(out), "速率的定義是？") {
		t.Fatalf("the student PDF has the questions and no answers:\n%s", out)
	}
}
