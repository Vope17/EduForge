package research

import (
	"fmt"
	"strings"
	"testing"

	"eduforge/internal/domain"
)

func pages(n int, textRunes int, mode string) []domain.Unit {
	out := []domain.Unit{}
	for i := 1; i <= n; i++ {
		out = append(out, domain.Unit{ID: fmt.Sprintf("s:p%d", i), SourceID: "s", SourceName: "lecture.pdf", Locator: i, Kind: "page", Text: strings.Repeat("x", textRunes), Mode: mode, Preview: fmt.Sprintf("p%d.png", i)})
	}
	return out
}

func TestEveryPageLandsInExactlyOneBatch(t *testing.T) {
	units := pages(10, 100, "text")
	units = append(units, domain.Unit{ID: "s:p11", SourceID: "s", Locator: 11, Mode: "vision"}) // a scan with no text
	units = append(units, pages(3, 20000, "text")...)                                           // dense pages
	for i := range units {
		units[i].ID = fmt.Sprintf("u%d", i)
	}
	batches := PlanBatches(units)
	seen := map[string]int{}
	for _, b := range batches {
		if len(b.Units) == 0 || len(b.Units) > BatchPages {
			t.Fatalf("batch size %d", len(b.Units))
		}
		runes := 0
		for _, u := range b.Units {
			seen[u.ID]++
			runes += min(len([]rune(u.Text)), BatchUnitRunes)
		}
		if len(b.Units) > 1 && runes > BatchTextRunes {
			t.Fatalf("a batch of %d pages carries %d runes of text", len(b.Units), runes)
		}
	}
	for _, u := range units {
		if seen[u.ID] != 1 {
			t.Fatalf("page %s is in %d batches", u.ID, seen[u.ID])
		}
	}
	if len(PlanBatches(nil)) != 0 {
		t.Fatal("no pages, no batches")
	}
}

func TestBatchPromptCarriesPagesVisionNoticeAndBackground(t *testing.T) {
	units := []domain.Unit{
		{ID: "s:p1", SourceName: "lecture.pdf", Locator: 1, Text: "Newton's second law: F = ma.", Mode: "text", Preview: "a.png"},
		{ID: "s:p2", SourceName: "lecture.pdf", Locator: 2, Text: "", Mode: "vision", Preview: "b.png"},
	}
	prompt := BatchPrompt(BatchRequest{Lang: "zh-TW", Title: "Forces", Course: "Physics I", Audience: "first-year students", Batch: Batch{Units: units}, Index: 2, Total: 5, Background: []string{"vector", "velocity"}})
	for _, want := range []string{"Traditional Chinese", "untrusted data", "[s:p1] lecture.pdf · 1 (text)", "[s:p2] lecture.pdf · 2 (little or no text; read the image)", "pages 1–2", "batch 2 of 5", "s:p1, s:p2", "which you must read from their image: s:p2", "Do not explain them again", "- vector", "Physics I", "first-year students", "learning objectives", "worked examples"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(BatchPrompt(BatchRequest{Lang: "en", Batch: Batch{Units: units[:1]}, Index: 1, Total: 1}), "Do not explain them again") {
		t.Fatal("no background, no background section")
	}
}

func TestParseBatchChecksCitationsNumbersAndVisionPages(t *testing.T) {
	batch := Batch{Units: []domain.Unit{
		{ID: "s:p1", Locator: 1, Text: "A car travels 120 km in 2 hours, so its speed is 60 km/h.", Mode: "text"},
		{ID: "s:p2", Locator: 2, Text: "", Mode: "vision"},
	}}
	reply := "Here you go:\n" + `{"sections":[
	 {"section":0,"points":[{"text":"算出平均速率。[s:p1]","citations":["s:p1"]}]},
	 {"section":4,"points":[
	   {"text":"一輛車 2 小時行駛 120 km。","detail":"速率 = 120 / 2 = 60 km/h，答案 75 km/h。","citations":["[s:p1]"]},
	   {"text":"圖上的斜率為 3.5。","citations":["s:p2"]},
	   {"text":"沒有出處的說法","citations":["other:p9"]}]},
	 {"section":9,"points":[{"text":"錯誤章節","citations":["s:p1"]}]}],
	 "terms":[{"original":"speed","en":"speed","zh-TW":"速率","definition":"單位時間的距離","citations":["s:p1"]},{"original":"ghost","citations":["nope"]}]}`
	got, err := ParseBatch(reply, batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Claims) != 3 {
		t.Fatalf("points with unknown sections or without a cited page of the batch are dropped: %+v", got.Claims)
	}
	if got.Claims[0].Text != "算出平均速率。" || got.Claims[0].Kind != "ai_summary" || got.Claims[0].Vision {
		t.Fatalf("citation markers are stripped: %+v", got.Claims[0])
	}
	if strings.Join(got.Claims[1].Unsupported, ",") != "75" {
		t.Fatalf("a number in the detail that the cited page lacks is flagged: %+v", got.Claims[1])
	}
	if got.Claims[1].Detail == "" || got.Claims[1].Section != SecExamples {
		t.Fatalf("example keeps its solution in detail: %+v", got.Claims[1])
	}
	if !got.Claims[2].Vision || len(got.Claims[2].Unsupported) != 0 {
		t.Fatalf("a point citing a page read from its image is marked, and its numbers are left to the reader: %+v", got.Claims[2])
	}
	if len(got.Terms) != 1 || got.Terms[0].Original != "speed" {
		t.Fatalf("terms need a cited page: %+v", got.Terms)
	}
	if empty, err := ParseBatch(`{"sections":[]}`, batch); err != nil || len(empty.Claims) != 0 {
		t.Fatalf("a batch with nothing to teach is valid: %+v %v", empty, err)
	}
	if _, err := ParseBatch(`not json`, batch); err == nil {
		t.Fatal("a reply without JSON is an error so the batch can be retried or missed")
	}
}

func TestAssembleMergesTheSamePointAcrossBatches(t *testing.T) {
	k := domain.Knowledge{Title: "Forces", Units: []domain.Unit{{ID: "a"}, {ID: "b"}}}
	first := Partial{
		Claims: []domain.Claim{{Section: SecConcepts, Text: "Force is a push or a pull that changes motion.", Citations: []string{"a"}}, {Section: SecFormulas, Text: "F = ma", Citations: []string{"a"}}},
		Terms:  []domain.Term{{Original: "Force", Citations: []string{"a"}}},
	}
	second := Partial{
		Claims: []domain.Claim{
			{Section: SecConcepts, Text: "Force is a push or a pull that changes motion. Its unit is the newton.", Detail: "SI unit: N", Citations: []string{"b"}},
			{Section: SecExamples, Text: "F = ma", Citations: []string{"b"}}, // same words, other section: kept
		},
		Terms: []domain.Term{{Original: "force", Definition: "a push or a pull", Citations: []string{"b"}}},
	}
	got := Assemble(k, []Partial{first, second})
	if len(got.Claims) != 3 {
		t.Fatalf("claims = %+v", got.Claims)
	}
	merged := got.Claims[0]
	if !strings.Contains(merged.Text, "newton") || merged.Detail != "SI unit: N" || strings.Join(merged.Citations, ",") != "a,b" {
		t.Fatalf("the fuller point is kept with both citations: %+v", merged)
	}
	if len(got.Terms) != 1 || got.Terms[0].Definition != "a push or a pull" || len(got.Terms[0].Citations) != 2 {
		t.Fatalf("terms merge by name, ignoring case: %+v", got.Terms)
	}
	if got.Author != "ai" {
		t.Fatalf("author = %q", got.Author)
	}
	// Assemble adds to what k already holds.
	more := Assemble(got, []Partial{{Claims: []domain.Claim{{Section: SecMisconceptions, Text: "Heavier objects do not fall faster.", Citations: []string{"b"}}}}})
	if len(more.Claims) != 4 {
		t.Fatalf("existing claims are kept: %d", len(more.Claims))
	}
}

func TestCoverageNamesTheMissedPages(t *testing.T) {
	units := pages(5, 50, "text")
	units[2].Mode = "vision"
	c := CountCoverage(units, map[string]bool{"s:p1": true, "s:p2": true, "s:p3": true}, 2)
	if c.Pages != 5 || c.Read != 3 || c.Vision != 1 || c.Batches != 2 || strings.Join(c.Missed, ",") != "s:p4,s:p5" || c.Complete() {
		t.Fatalf("coverage = %+v", c)
	}
	all := map[string]bool{}
	for _, u := range units {
		all[u.ID] = true
	}
	if !CountCoverage(units, all, 2).Complete() {
		t.Fatal("every page read is complete")
	}
	var none *domain.Coverage
	if none.Complete() {
		t.Fatal("no coverage is not complete")
	}
}

func TestBackgroundListsEarlierConceptsOnce(t *testing.T) {
	earlier := []domain.Knowledge{
		{Terms: []domain.Term{{Original: "vector"}, {Original: "scalar"}}, Claims: []domain.Claim{{Section: SecConcepts, Text: "A vector has magnitude and direction."}, {Section: SecFormulas, Text: "ignored: not a concept"}}},
		{Terms: []domain.Term{{Original: "Vector"}}, Claims: []domain.Claim{{Section: SecConcepts, Text: "A vector has magnitude and direction."}}},
	}
	got := BackgroundConcepts(earlier)
	if strings.Join(got, "|") != "vector|scalar|A vector has magnitude and direction." {
		t.Fatalf("background = %v", got)
	}
}

func TestModelNamesTheDocumentByTheTitleItPrints(t *testing.T) {
	k := domain.Knowledge{Title: "untitled", Units: []domain.Unit{
		{ID: "d:p1", SourceID: "d", SourceName: "slides_final_v3.pdf", Locator: 1, Text: "Introduction to\nNewtonian Mechanics\nWeek 3"},
		{ID: "d:p2", SourceID: "d", SourceName: "slides_final_v3.pdf", Locator: 2, Text: "Agenda."},
		{ID: "e:p1", SourceID: "e", SourceName: "notes.pdf", Locator: 1, Text: "Some notes without a title."},
	}}
	got := Assemble(k, []Partial{{Titles: []Title{
		{Source: "d:p2", Text: "Introduction to Newtonian Mechanics"},
		{Source: "e:p1", Text: "A Title The Model Made Up"},
	}}})
	if got.Units[0].SourceTitle != "Introduction to Newtonian Mechanics" || got.Units[1].SourceTitle != got.Units[0].SourceTitle {
		t.Fatalf("the printed title names every page of its document: %+v", got.Units[:2])
	}
	if got.Units[2].SourceTitle != "" {
		t.Fatalf("a title the pages do not print is ignored: %+v", got.Units[2])
	}
	if k.Units[0].SourceTitle != "" {
		t.Fatal("the input knowledge is not changed")
	}
}
