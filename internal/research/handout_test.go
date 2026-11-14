package research

import (
	"strings"
	"testing"

	"eduforge/internal/domain"
)

func claim(section int, text, detail string, cites ...string) domain.Claim {
	return domain.Claim{Section: section, Text: text, Detail: detail, Citations: cites, Kind: "ai_summary", Verified: true}
}

func TestHandoutPartsFollowTheKnowledgeThatFeedsThem(t *testing.T) {
	onlyConcepts := domain.Knowledge{Claims: []domain.Claim{claim(SecConcepts, "Speed is distance over time.", "", "a")}}
	keys := []string{}
	for _, i := range HandoutPlan(onlyConcepts) {
		keys = append(keys, HandoutParts[i].Key)
	}
	if strings.Join(keys, ",") != "summary,concepts,review,exercises" {
		t.Fatalf("parts without evidence (examples, misconceptions) are not written: %v", keys)
	}
	full := domain.Knowledge{Claims: []domain.Claim{claim(0, "a", ""), claim(1, "b", ""), claim(2, "c", ""), claim(3, "d", ""), claim(4, "e", ""), claim(5, "f", ""), claim(6, "g", "")}}
	if len(HandoutPlan(full)) != len(HandoutParts) {
		t.Fatal("every part has evidence")
	}
	if len(HandoutPlan(domain.Knowledge{})) != 0 {
		t.Fatal("no knowledge, no handout")
	}
	for _, key := range []string{"summary", "concepts", "examples", "misconceptions", "review", "exercises"} {
		if PartTitle(key, "zh-TW") == "" || PartTitle(key, "en") == "" || partBriefs[key] == "" || PartIndex(key) < 0 {
			t.Fatalf("part %s is incomplete", key)
		}
	}
	if PartTitle("examples", "zh-TW") != "例題與詳解" {
		t.Fatal("the required part names are the plan's")
	}
}

func TestPartExcerptCarriesDetailsAndCitations(t *testing.T) {
	k := domain.Knowledge{Claims: []domain.Claim{
		claim(SecExamples, "A car travels 120 km in 2 hours.", "v = 120 / 2 = 60 km/h", "p1"),
		claim(SecExamples, "A runner runs 10 km in 50 minutes.", "", "p1", "p2"),
		claim(SecMisconceptions, "Average speed is the mean of two speeds.", "Use total distance over total time.", "p3"),
	}}
	got := PartExcerpt(k, HandoutParts[PartIndex("examples")], "standard", "zh-TW")
	if got.Part != "examples" || got.Title != "例題與詳解" || strings.Join(got.Citations, ",") != "p1,p2" {
		t.Fatalf("excerpt = %+v", got)
	}
	if !strings.Contains(got.Body, "→ v = 120 / 2 = 60 km/h") || strings.Contains(got.Body, "Average speed") {
		t.Fatalf("an example's solution rides along and other sections stay out:\n%s", got.Body)
	}
	empty := PartExcerpt(domain.Knowledge{}, HandoutParts[0], "standard", "en")
	if !strings.Contains(empty.Body, "missing") {
		t.Fatalf("no evidence says so: %q", empty.Body)
	}
}

func TestPartSpanScalesWithDepthAndWeight(t *testing.T) {
	deep, brief := PartSpan("deep", "zh-TW", HandoutParts[1]), PartSpan("brief", "zh-TW", HandoutParts[1])
	if deep != "1600 to 2800 Chinese characters" || brief != "300 to 500 Chinese characters" {
		t.Fatalf("concepts carry the full length: %s / %s", deep, brief)
	}
	if got := PartSpan("standard", "zh-TW", HandoutParts[0]); got != "245 to 420 Chinese characters" {
		t.Fatalf("a short part is a fraction of it: %s", got)
	}
	if got := PartSpan("standard", "en", HandoutParts[1]); got != "350 to 600 English words" {
		t.Fatalf("English counts half as many words: %s", got)
	}
}

func TestHandoutValidationFlagsAnswersAndMissingParts(t *testing.T) {
	k := domain.Knowledge{
		Units:  []domain.Unit{{ID: "p1", Text: "Speed is distance over time."}},
		Claims: []domain.Claim{claim(SecConcepts, "Speed is distance over time.", "", "p1"), claim(SecExamples, "A car travels 120 km.", "", "p1")},
	}
	sec := func(part, body string) domain.Section {
		return domain.Section{Title: PartTitle(part, "zh-TW"), Part: part, Body: body, Citations: []string{"p1"}}
	}
	good := domain.Artifact{Type: "A", Language: "zh-TW", Mode: "model", Sections: []domain.Section{sec("summary", "s"), sec("concepts", "c"), sec("examples", "e"), sec("review", "r"), sec("exercises", "1. 一輛車 2 小時行駛 120 公里，速率是多少？")}}
	if check := Validate(good, k); check.Status != "checked" {
		t.Fatalf("a complete handout without answers is checked: %+v", check.Issues)
	}
	leaky := good
	leaky.Sections = append([]domain.Section(nil), good.Sections...)
	leaky.Sections[4] = sec("exercises", "1. 速率是多少？\n答案：60 公里／小時")
	found := false
	for _, is := range Validate(leaky, k).Issues {
		found = found || is.Code == "answer_in_exercises"
	}
	if !found {
		t.Fatal("answers in the practice problems must be flagged")
	}
	missing := good
	missing.Sections = good.Sections[:3] // no review, no exercises
	codes := []string{}
	for _, is := range Validate(missing, k).Issues {
		if is.Code == "handout_part_missing" {
			codes = append(codes, is.Detail)
		}
	}
	if strings.Join(codes, ",") != "review,exercises" {
		t.Fatalf("missing required parts are named: %v", codes)
	}
	extract := missing
	extract.Mode = "source-extract"
	for _, is := range Validate(extract, k).Issues {
		if is.Code == "handout_part_missing" {
			t.Fatal("an extractive draft is flagged as a draft, not for missing parts")
		}
	}
	for _, body := range []string{"Answer: 5", "Solution: x=2", "解答：略", "answer key"} {
		if !exercisesGiveAnswers(body) {
			t.Errorf("%q is an answer", body)
		}
	}
	if exercisesGiveAnswers("1. Find the answer to the equation x+1=2 by yourself.") {
		t.Error("the word answer inside a problem is not an answer")
	}
}

func TestSlidePlanScalesToTheClass(t *testing.T) {
	if got := SlideTotal(50); got != 28 {
		t.Fatalf("a 50-minute class has about 28 slides, got %d", got)
	}
	if SlideTotal(0) != 28 || SlideTotal(5) != 6 || SlideTotal(180) != 60 {
		t.Fatalf("defaults and limits: %d %d %d", SlideTotal(0), SlideTotal(5), SlideTotal(180))
	}
	k := domain.Knowledge{Claims: []domain.Claim{claim(SecExamples, "Example", "Solution: 60", "p1"), claim(SecConcepts, "Concept", "", "p1")}, Units: []domain.Unit{{ID: "p1", Text: "text"}}}
	prompt := OutlinePrompt(k, domain.OutputSettings{Minutes: 50, Level: "beginner", Audience: "first-year students"}, "zh-TW", []string{"vector"})
	for _, want := range []string{"50-minute class session", "Plan exactly 28 content slides", "first content slide states them", "The class: first-year students.", "new to the subject", "do not give them slides of their own", "vector", "→ Solution: 60", "[例題與解答] Example"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("outline prompt lacks %q:\n%s", want, prompt)
		}
	}
}
