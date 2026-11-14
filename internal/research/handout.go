package research

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"eduforge/internal/domain"
)

// A handout (output "A") has fixed parts, each written from the knowledge
// sections that feed it. The parts are required: a handout without a part its
// knowledge could fill is flagged by Validate.

// Part is one required part of a handout.
type Part struct {
	Key      string
	Sections []int   // knowledge sections that feed it
	Weight   float64 // its share of the handout's length
}

// HandoutParts is the structure of a handout, in order.
var HandoutParts = []Part{
	{"summary", []int{SecObjectives, SecConcepts}, 0.35},
	{"concepts", []int{SecPrerequisites, SecConcepts, SecFormulas, SecFigures}, 1.0},
	{"examples", []int{SecExamples}, 0.8},
	{"misconceptions", []int{SecMisconceptions}, 0.5},
	{"review", []int{SecObjectives, SecConcepts, SecFormulas}, 0.4},
	{"exercises", []int{SecExamples, SecConcepts, SecFormulas}, 0.5},
}

var partTitles = map[string]map[string]string{
	"zh-TW": {"summary": "本節重點", "concepts": "概念講解", "examples": "例題與詳解", "misconceptions": "常見迷思", "review": "重點整理", "exercises": "課後練習"},
	"en":    {"summary": "Key points", "concepts": "Concepts explained", "examples": "Worked examples", "misconceptions": "Common misconceptions", "review": "Review", "exercises": "Practice problems"},
}

// PartTitle is a part's heading in lang.
func PartTitle(key, lang string) string {
	if lang != "en" {
		lang = "zh-TW"
	}
	return partTitles[lang][key]
}

// PartIndex finds a part by key, or -1.
func PartIndex(key string) int {
	for i, p := range HandoutParts {
		if p.Key == key {
			return i
		}
	}
	return -1
}

// partHasEvidence reports whether the knowledge has points for a part. The
// review and exercises need something to review or practise, so they follow
// the sections that feed them like any other part.
func partHasEvidence(k domain.Knowledge, p Part) bool {
	for _, c := range k.Claims {
		for _, s := range p.Sections {
			if c.Section == s {
				return true
			}
		}
	}
	return false
}

// HandoutPlan lists the parts (by index into HandoutParts) a handout can be
// written for from this knowledge.
func HandoutPlan(k domain.Knowledge) []int {
	var plan []int
	for i, p := range HandoutParts {
		if partHasEvidence(k, p) {
			plan = append(plan, i)
		}
	}
	return plan
}

// partBrief tells the model what a part is for and how to write it.
var partBriefs = map[string]string{
	"summary":        "The handout's opening: what this lesson is about and what students will be able to do afterwards. Write one bullet per learning objective (start with an action verb), then one short paragraph on the lesson's main idea. No new detail: later parts explain.",
	"concepts":       "The lesson's explanation. Go through the concepts in the order the material presents them: for each give the definition, then the intuition or an example from the material, and for each formula its symbols and when it applies. Group them under subheadings. Mention a prerequisite only where students need it.",
	"examples":       "The worked examples. For each: the problem statement, then the solution step by step, then the answer, as the material gives them. If the material gives no answer or solution for an example, say so in one sentence and leave it unsolved; never solve it yourself.",
	"misconceptions": "The common misconceptions and pitfalls. For each: the mistake, why it is tempting, and the correct understanding.",
	"review":         "A concise recap students can revise from: a bullet list of the key ideas, definitions and formulas to remember. Nothing new beyond the lesson.",
	"exercises":      "Practice problems for after class: 3 to 6 numbered problems (1., 2., 3.) based on this lesson's concepts, formulas and examples, in increasing difficulty, each solvable from this lesson alone. Write the problem statements only: NO answers, solutions, hints that give the answer, or answer keys.",
}

// PartExcerpt gathers a part's key points, with their details, grouped by
// knowledge section, and the pages they cite, for the part's prompt.
func PartExcerpt(k domain.Knowledge, p Part, depth, lang string) domain.Section {
	limit := 3000
	switch depth {
	case "brief":
		limit = 1500
	case "deep":
		limit = 8000
	}
	var text []string
	var cites []string
	seen := map[string]bool{}
	titles := Titles[lang]
	if titles == nil {
		titles = Titles["zh-TW"]
	}
	for _, s := range p.Sections {
		var lines []string
		for _, c := range k.Claims {
			if c.Section != s {
				continue
			}
			line := "- " + strings.TrimSpace(c.Text)
			if d := strings.TrimSpace(c.Detail); d != "" {
				line += "\n  → " + strings.ReplaceAll(d, "\n", "\n    ")
			}
			lines = append(lines, line)
			for _, cite := range c.Citations {
				if !seen[cite] {
					seen[cite] = true
					cites = append(cites, cite)
				}
			}
		}
		if len(lines) > 0 {
			text = append(text, titles[s]+":\n"+strings.Join(lines, "\n"))
		}
	}
	body := strings.Join(text, "\n\n")
	if body == "" {
		if lang == "en" {
			body = "Source information is missing; review required."
		} else {
			body = "來源資訊不足，需使用者確認。"
		}
	}
	return domain.Section{Title: PartTitle(p.Key, lang), Part: p.Key, Body: Clip(body, limit), Citations: cites}
}

// PartSpan states how long a part's body should be, as a length in Chinese
// characters (half as many English words) scaled by the part's weight.
func PartSpan(depth, lang string, p Part) string {
	b := BudgetFor("A", depth)
	lo, hi := int(math.Round(float64(b.BodyMin)*p.Weight)), int(math.Round(float64(b.BodyMax)*p.Weight))
	if lang == "en" {
		return fmt.Sprintf("%d to %d English words", max(1, lo/2), max(1, hi/2))
	}
	return fmt.Sprintf("%d to %d Chinese characters", lo, hi)
}

var answerMarker = regexp.MustCompile(`(?i)(答案|解答|解析|answer\s*key|answers?\s*[:：]|solutions?\s*[:：]|\bans\.)`)

// exercisesGiveAnswers reports whether practice problems contain an answer.
func exercisesGiveAnswers(body string) bool { return answerMarker.MatchString(body) }
