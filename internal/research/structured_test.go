package research

import (
	"strings"
	"testing"

	"eduforge/internal/domain"
)

func lessonKnowledge() domain.Knowledge {
	return domain.Knowledge{
		Title: "Speed",
		Units: []domain.Unit{{ID: "p1", Text: "Speed is distance over time.", Locator: 1}, {ID: "p2", Text: "A car travels 120 km in 2 hours.", Locator: 2}},
		Claims: []domain.Claim{
			{Section: SecObjectives, Text: "Compute average speed.", Citations: []string{"p1"}, Verified: true},
			{Section: SecObjectives, Text: "Explain why speed is a rate.", Citations: []string{"p1"}, Verified: true},
			{Section: SecConcepts, Text: "Speed is distance over time.", Citations: []string{"p1"}, Verified: true},
			{Section: SecExamples, Text: "A car travels 120 km in 2 hours.", Detail: "v = 60 km/h", Citations: []string{"p2"}, Verified: true},
		},
	}
}

const goodPlan = `{"goal":"Students compute average speed.","analysis":"The lesson defines speed.","experience":"They know division.",
 "objectives":[{"code":"O1","domain":"skill","text":"Compute average speed."},{"code":"O2","domain":"cognitive","text":"Explain why speed is a rate."}],
 "activities":[
  {"phase":"prepare","title":"Warm-up","description":"Ask about commutes.","minutes":10,"objectives":["O2"],"resources":"board","assessment":"questions"},
  {"phase":"develop","title":"Teach speed","description":"Define and work the car example.","minutes":30,"objectives":["O1","O2"],"resources":"slides","assessment":"exercise"},
  {"phase":"wrap","title":"Recap","description":"Summarise.","minutes":10,"objectives":["O1"],"resources":"handout","assessment":"exit ticket"}],
 "references":["Speed lecture"],"citations":["p1","p2","nope"]}`

func codesOf(issues []domain.Issue) string {
	out := []string{}
	for _, i := range issues {
		out = append(out, i.Code+":"+i.Detail)
	}
	return strings.Join(out, ",")
}

func TestObjectiveCodesNumberTheKnowledgeObjectives(t *testing.T) {
	got := ObjectiveCodes(lessonKnowledge())
	if len(got) != 2 || got[0].Code != "O1" || got[1].Code != "O2" || got[1].Text != "Explain why speed is a rate." {
		t.Fatalf("codes = %+v", got)
	}
}

func TestPlanPromptCarriesTheClassTimeObjectivesAndKnowledge(t *testing.T) {
	prompt := PlanPrompt(PlanRequest{Knowledge: lessonKnowledge(), Info: domain.PlanInfo{Unit: "Speed", Minutes: 50}, Lang: "zh-TW", Settings: domain.OutputSettings{Audience: "first-year students", Level: "beginner", Instructions: map[string]string{"C": "Use group work."}}, Known: []string{"division"}})
	for _, want := range []string{"50 minutes", "MUST add up to exactly 50", "O1: Compute average speed.", "The class: first-year students.", "new to the subject", "Use group work.", "division", "→ v = 60 km/h", "Traditional Chinese", "[p2]", "prepare|develop|wrap"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("plan prompt lacks %q:\n%s", want, prompt)
		}
	}
}

func TestParsePlanKeepsOnlyWhatIsValidAndTakesInfoFromTheLesson(t *testing.T) {
	info := domain.PlanInfo{Unit: "Speed", Minutes: 50, Audience: "class"}
	p, err := ParsePlan("Here:\n"+goodPlan, lessonKnowledge(), info)
	if err != nil {
		t.Fatal(err)
	}
	if p.Info != info || len(p.Objectives) != 2 || len(p.Activities) != 3 || strings.Join(p.Citations, ",") != "p1,p2" {
		t.Fatalf("plan = %+v", p)
	}
	if issues := CheckPlan(p, 50); len(issues) != 0 {
		t.Fatalf("a good plan has no issues: %s", codesOf(issues))
	}
	bad := strings.Replace(goodPlan, `"phase":"wrap"`, `"phase":"party"`, 1)
	bad = strings.Replace(bad, `"domain":"skill"`, `"domain":"vibes"`, 1)
	p, err = ParsePlan(bad, lessonKnowledge(), info)
	if err != nil || len(p.Activities) != 2 || p.Objectives[0].Domain != "cognitive" {
		t.Fatalf("an unknown phase drops the activity and an unknown domain defaults: %+v %v", p, err)
	}
	if _, err = ParsePlan(`{"goal":"x","objectives":[],"activities":[]}`, lessonKnowledge(), info); err == nil {
		t.Fatal("a plan with no objectives or activities is rejected")
	}
	if _, err = ParsePlan("no json", lessonKnowledge(), info); err == nil {
		t.Fatal("not JSON")
	}
}

func TestCheckPlanReportsTimeObjectivesAndPhasesWithoutFixingThem(t *testing.T) {
	p, _ := ParsePlan(goodPlan, lessonKnowledge(), domain.PlanInfo{Minutes: 50})
	p.Activities[1].Minutes = 25 // 45 in total
	p.Activities[2].Objectives = []string{"O9"}
	p.Activities[0].Objectives = nil
	p.Activities[1].Objectives = []string{"O1"} // O2 now unserved
	got := codesOf(CheckPlan(p, 50))
	for _, want := range []string{"plan_time_mismatch:45/50", "plan_unknown_objective:O9", "plan_objective_unlinked:O2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	if p.Activities[1].Minutes != 25 {
		t.Fatal("the check must not change the plan")
	}
	p.Activities = p.Activities[:2] // no wrap phase
	if got = codesOf(CheckPlan(p, 50)); !strings.Contains(got, "plan_phase_missing:wrap") {
		t.Fatalf("a plan without a wrap-up: %s", got)
	}
	p.Citations = nil
	if got = codesOf(CheckPlan(p, 50)); !strings.Contains(got, "missing_citation") {
		t.Fatalf("a plan without pages cited: %s", got)
	}
}

func TestFallbackPlanFillsTheClassTimeAndIsMarkedByTheCaller(t *testing.T) {
	p := FallbackPlan(lessonKnowledge(), domain.PlanInfo{Unit: "Speed", Minutes: 50}, "zh-TW")
	total := 0
	for _, a := range p.Activities {
		total += a.Minutes
	}
	if total != 50 || len(p.Objectives) != 2 || len(p.Activities) != 3 {
		t.Fatalf("draft plan: %d minutes, %+v", total, p)
	}
	if issues := CheckPlan(p, 50); strings.Contains(codesOf(issues), "time") || strings.Contains(codesOf(issues), "phase") {
		t.Fatalf("the draft's time and phases are right: %s", codesOf(issues))
	}
	tiny := FallbackPlan(lessonKnowledge(), domain.PlanInfo{Minutes: 1}, "en")
	if len(tiny.Activities) != 3 {
		t.Fatal("a tiny class still has all phases")
	}
}

const goodSheet = `{"title":"Speed worksheet","questions":[
 {"type":"choice","prompt":"Speed is…","choices":["distance/time","time/distance","distance*time","none"],"answer":"A","explanation":"definition","objective":"O1","points":2,"citations":["p1"]},
 {"type":"fill","prompt":"Speed = ____ / time","answer":"distance","explanation":"definition","objective":"O1","points":1,"citations":["p1"]},
 {"type":"calc","prompt":"A car travels 120 km in 2 hours. Find its speed.","answer":"60 km/h","explanation":"120/2","objective":"O1","points":3,"citations":["p2"]}]}`

func TestWorksheetPromptAsksForTheMixAndTheObjectives(t *testing.T) {
	mix := map[string]int{"choice": 1, "fill": 1, "calc": 1}
	prompt := WorksheetPrompt(lessonKnowledge(), "en", domain.OutputSettings{Audience: "first-year students"}, mix, nil)
	for _, want := range []string{"- choice: 1", "- fill: 1", "- calc: 1", "O2: Explain why speed is a rate.", "exactly one correct", "English", "The class: first-year students."} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("worksheet prompt lacks %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "- short:") || strings.Contains(prompt, "- practice:") {
		t.Fatal("types with no questions are not asked for")
	}
	def := DefaultQuestions(lessonKnowledge())
	if def["choice"] != 4 || def["calc"] != 2 {
		t.Fatalf("a lesson with formulas or examples gets calculation questions: %v", def)
	}
	if DefaultQuestions(domain.Knowledge{})["calc"] != 0 {
		t.Fatal("no formulas, no calculations")
	}
}

func TestWorksheetIsCheckedAndTheStudentCopyHasNoAnswers(t *testing.T) {
	w, err := ParseWorksheet(goodSheet, lessonKnowledge())
	if err != nil {
		t.Fatal(err)
	}
	mix := map[string]int{"choice": 1, "fill": 1, "calc": 1}
	if issues := CheckWorksheet(w, lessonKnowledge(), mix); len(issues) != 0 {
		t.Fatalf("a good worksheet has no issues: %s", codesOf(issues))
	}
	student := StudentCopy(w)
	text := domain.Encode(student)
	for _, secret := range []string{"60 km/h", "definition", "120/2", `"answer":"A"`} {
		if strings.Contains(string(text), secret) {
			t.Fatalf("the student copy leaks %q: %s", secret, text)
		}
	}
	if len(student.Questions) != 3 || student.Questions[0].Choices[0] != "distance/time" || w.Questions[2].Answer != "60 km/h" {
		t.Fatalf("the student copy keeps the questions; the original keeps its answers: %+v", student)
	}
	w.Questions[0].Answer = "E"
	w.Questions[1].Answer = ""
	w.Questions[2].Objective = "O7"
	w.Questions[2].Citations = nil
	got := codesOf(CheckWorksheet(w, lessonKnowledge(), map[string]int{"choice": 2, "fill": 1, "calc": 1}))
	for _, want := range []string{"question_bad_choice", "question_no_answer", "question_no_objective:O7", "question_no_source", "worksheet_count_mismatch:choice 1/2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	if _, err = ParseWorksheet(`{"questions":[{"type":"riddle","prompt":"?"}]}`, lessonKnowledge()); err == nil {
		t.Fatal("unknown question types are dropped, leaving nothing")
	}
}

func TestSyllabusIsParsedCheckedAndHasAFallback(t *testing.T) {
	lessons := []SyllabusLesson{{Chapter: "One", Title: "Speed"}, {Chapter: "One", Title: "Velocity"}, {Chapter: "Two", Title: "Force"}}
	prompt := SyllabusPrompt("Physics I", "first-year students", 4, lessons, "zh-TW", "")
	for _, want := range []string{"over 4 weeks", `exactly 4 rows`, "1. [One] Speed", "3. [Two] Force", "first-year students"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("syllabus prompt lacks %q", want)
		}
	}
	reply := `{"description":"d","objectives":["o"],"schedule":[{"week":1,"topic":"Speed","chapters":"One","method":"lecture","assessment":""},{"week":2,"topic":"Force","chapters":"Two","method":"lab","assessment":"quiz"},{"week":9,"topic":"out of range"}],"grading":[{"item":"Exam","percent":60},{"item":"Homework","percent":30}],"books":["Physics"]}`
	s, err := ParseSyllabus(reply, "Physics I", "class", 4)
	if err != nil || len(s.Schedule) != 2 {
		t.Fatalf("rows outside the weeks are dropped: %+v %v", s, err)
	}
	got := codesOf(CheckSyllabus(s))
	for _, want := range []string{"syllabus_week_missing:3", "syllabus_week_missing:4", "syllabus_grading_total:90"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	fb := FallbackSyllabus("Physics I", "class", 4, lessons)
	if len(fb.Schedule) != 4 || fb.Schedule[0].Topic == "" || fb.Schedule[3].Topic == "" {
		t.Fatalf("every week has a row: %+v", fb.Schedule)
	}
	all := ""
	for _, w := range fb.Schedule {
		all += w.Topic + "|"
	}
	for _, l := range lessons {
		if !strings.Contains(all, l.Title) {
			t.Fatalf("lesson %s is missing from the fallback schedule: %s", l.Title, all)
		}
	}
	if issues := CheckSyllabus(fb); len(issues) != 0 {
		t.Fatalf("fallback: %s", codesOf(issues))
	}
}

func TestValidateRoutesStructuredOutputs(t *testing.T) {
	k := lessonKnowledge()
	p, _ := ParsePlan(goodPlan, k, domain.PlanInfo{Minutes: 50})
	check := Validate(domain.Artifact{Type: "C", Plan: &p, Mode: "model"}, k)
	if check.Status != "checked" {
		t.Fatalf("a good plan on verified knowledge is checked: %+v", check.Issues)
	}
	check = Validate(domain.Artifact{Type: "C", Plan: &p, Mode: "source-extract"}, k)
	if check.Status != "draft" || !strings.Contains(codesOf(check.Issues), "extractive_draft") {
		t.Fatalf("an extractive draft is a draft: %+v", check)
	}
	if Validate(domain.Artifact{Type: "D"}, k).Status != "draft" {
		t.Fatal("an empty worksheet is not checked")
	}
}
