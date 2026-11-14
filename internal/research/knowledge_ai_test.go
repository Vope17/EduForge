package research

import (
	"strings"
	"testing"

	"eduforge/internal/domain"
)

func TestModelKnowledgeIsCheckedAgainstItsSources(t *testing.T) {
	k := domain.Knowledge{Title: "Study", Units: []domain.Unit{
		{ID: "s:p1", SourceName: "lesson.pdf", Locator: 1, Text: "We propose AIDA. Accuracy reaches 95.4% on 846 cases."},
		{ID: "s:p2", SourceName: "lesson.pdf", Locator: 2, Text: "Limitations: tested on one cloud only."},
	}, Terms: []domain.Term{{Original: "rule term"}}}
	prompt := KnowledgePrompt(k, "zh-TW")
	for _, want := range []string{"[s:p1] lesson.pdf · 1", "Traditional Chinese", "untrusted data", "results"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt lacks %q", want)
		}
	}
	reply := "Here you go:\n" + `{"sections":[
	 {"section":0,"points":[{"text":"AIDA 針對設備故障的根因分析。[s:p1]","citations":["s:p1"]}]},
	 {"section":4,"points":[{"text":"準確率 95.4%，共 846 個案例。","citations":["[s:p1]"]},{"text":"延遲降低 72.6 小時。","citations":["s:p1"]},{"text":"沒有出處的說法","citations":["nope"]}]},
	 {"section":5,"points":[{"text":"只在一個雲端測試。","citations":["s:p2"]}]},
	 {"section":9,"points":[{"text":"錯誤章節","citations":["s:p1"]}]}],
	 "terms":[{"original":"AIDA","en":"AIDA","zh-TW":"AIDA","definition":"系統名稱","citations":["x"]}],
	 "conditions":{"task":"root cause analysis","dataset":"846 production cases","metric":"accuracy"}}`
	got, err := ParseKnowledge(reply, k)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Claims) != 4 || got.Author != "ai" {
		t.Fatalf("points without known sections or citations are dropped: %+v", got.Claims)
	}
	if got.Claims[0].Text != "AIDA 針對設備故障的根因分析。" || got.Claims[0].Kind != "ai_summary" {
		t.Fatalf("citation markers stripped: %q", got.Claims[0].Text)
	}
	if got.Claims[1].Citations[0] != "s:p1" || len(got.Claims[1].Unsupported) != 0 {
		t.Fatalf("numbers found in the cited passage are supported: %+v", got.Claims[1])
	}
	if strings.Join(got.Claims[2].Unsupported, ",") != "72.6" {
		t.Fatalf("a number the source lacks is flagged: %+v", got.Claims[2])
	}
	if len(got.Terms) != 1 || got.Terms[0].Citations[0] != "s:p1" || got.Conditions.Dataset != "846 production cases" {
		t.Fatalf("terms and conditions: %+v %+v", got.Terms, got.Conditions)
	}
	if got.Units[0].SourceTitle != "" {
		t.Fatalf("no title given, none set: %+v", got.Units[0])
	}
	if _, err := ParseKnowledge(`{"sections":[]}`, k); err == nil {
		t.Fatal("a reply with too few points is rejected")
	}
}

func TestModelNamesTheDocumentByTheTitleItPrints(t *testing.T) {
	k := domain.Knowledge{Title: "asdasd", Units: []domain.Unit{
		{ID: "d:p1", SourceID: "d", SourceName: "AIDA_SIGCOMM2026_Lesson_Review.pdf", Locator: 1, Text: "PA P E R R E V I E W\n\nAIDA\nAccelerating Root Cause Analysis for\nMulti-Vendor Device Failures with\nLLM-Powered Reasoning\nHaoran Xu"},
		{ID: "d:p2", SourceID: "d", SourceName: "AIDA_SIGCOMM2026_Lesson_Review.pdf", Locator: 2, Text: "Agenda. Accuracy reaches 95.4%."},
		{ID: "e:p1", SourceID: "e", SourceName: "notes.pdf", Locator: 1, Text: "Some notes without a title."},
	}}
	if !strings.Contains(KnowledgePrompt(k, "zh-TW"), "Never use the file name") {
		t.Fatal("prompt asks for the printed title")
	}
	points := `"sections":[{"section":0,"points":[{"text":"a","citations":["d:p1"]},{"text":"b","citations":["d:p2"]},{"text":"c","citations":["d:p2"]},{"text":"d","citations":["e:p1"]}]}]`
	reply := `{` + points + `,"titles":[
	 {"source":"d:p2","title":"AIDA: Accelerating Root Cause Analysis for Multi-Vendor Device Failures with LLM-Powered Reasoning"},
	 {"source":"e:p1","title":"A Title The Model Made Up"}]}`
	got, err := ParseKnowledge(reply, k)
	if err != nil {
		t.Fatal(err)
	}
	want := "AIDA: Accelerating Root Cause Analysis for Multi-Vendor Device Failures with LLM-Powered Reasoning"
	if got.Units[0].SourceTitle != want || got.Units[1].SourceTitle != want {
		t.Fatalf("the printed title names every page of its document: %+v", got.Units[:2])
	}
	if got.Units[2].SourceTitle != "" {
		t.Fatalf("a title the pages do not print is ignored: %+v", got.Units[2])
	}
	if k.Units[0].SourceTitle != "" {
		t.Fatal("the input knowledge is not changed")
	}
}
