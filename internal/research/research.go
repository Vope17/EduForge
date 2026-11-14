// Package research supplies conservative extraction and mechanically checkable provenance.
package research

import (
	"regexp"
	"strings"
	"unicode"

	"eduforge/internal/domain"
)

var Titles = map[string][]string{
	"en":    {"Motivation & research question", "Core concepts & contributions", "Method & equations", "Experimental design", "Results", "Limitations", "Related work"},
	"zh-TW": {"研究動機與問題", "核心概念與貢獻", "方法與公式", "實驗設計", "研究結果", "限制與待確認事項", "相關研究"},
}
var sectionPatterns = []*regexp.Regexp{regexp.MustCompile(`(?i)intro|motivat|problem|動機|研究問題`), regexp.MustCompile(`(?i)abstract|contribut|concept|摘要|貢獻`), regexp.MustCompile(`(?i)method|algorithm|equation|方法|模型`), regexp.MustCompile(`(?i)experiment|dataset|evaluation|實驗|資料集`), regexp.MustCompile(`(?i)result|accuracy|結果`), regexp.MustCompile(`(?i)limit|conclu|future|限制|結論`), regexp.MustCompile(`(?i)related work|prior work|previous work|background|相關研究|相關工作|背景`)}

func Clip(text string, n int) string {
	r := []rune(text)
	if len(r) <= n {
		return text
	}
	return string(r[:n]) + "…"
}
func Extract(title string, sources []domain.Record) domain.Knowledge {
	k := domain.Knowledge{Title: title, Claims: []domain.Claim{}, Terms: []domain.Term{}, Units: []domain.Unit{}, Images: []domain.Image{}, Issues: []domain.Issue{}}
	for _, r := range sources {
		src, err := domain.Decode[domain.Source](r.Data)
		if err != nil || src.Role != "research" {
			continue
		}
		if src.Parsed == nil {
			k.Issues = append(k.Issues, domain.Issue{Code: "source_missing", Detail: r.ID})
			continue
		}
		for _, unit := range src.Parsed.Units {
			unit.SourceName = src.Name
			unit.SourceTitle = src.Parsed.Title
			k.Units = append(k.Units, unit)
		}
		k.Images = append(k.Images, src.Parsed.Images...)
		k.Issues = append(k.Issues, src.Parsed.Issues...)
	}
	for i, pattern := range sectionPatterns {
		count := 0
		for _, u := range k.Units {
			lines := strings.Split(u.Text, "\n")
			var excerpts []string
			for index, line := range lines {
				if !pattern.MatchString(line) {
					continue
				}
				text := strings.TrimSpace(line)
				if len([]rune(text)) < 45 && index+1 < len(lines) && strings.TrimSpace(lines[index+1]) != "" {
					text += "\n" + strings.TrimSpace(lines[index+1])
				}
				excerpts = append(excerpts, text)
				if len(excerpts) == 4 {
					break
				}
			}
			if len(excerpts) != 0 {
				k.Claims = append(k.Claims, domain.Claim{Section: i, Text: Clip(strings.Join(excerpts, "\n"), 3600), Citations: []string{u.ID}, Kind: "source_excerpt"})
				count++
				if count == 2 {
					break
				}
			}
		}
		if count == 0 && len(k.Units) > 0 {
			u := k.Units[min(i, len(k.Units)-1)]
			k.Claims = append(k.Claims, domain.Claim{Section: i, Text: Clip(u.Text, 900), Citations: []string{u.ID}, Kind: "source_excerpt"})
		}
	}
	k.Terms = ExtractTerms(k.Units, title)
	return k
}

var numberPattern = regexp.MustCompile(`[-+]?\b[0-9]+(?:\.[0-9]+)?(?:%|e[-+]?[0-9]+)?`)

func numberSet(text string) map[string]bool {
	out := map[string]bool{}
	for _, n := range numberPattern.FindAllString(text, -1) {
		out[n] = true
	}
	return out
}
func Validate(a domain.Artifact, k domain.Knowledge) domain.Check {
	check := domain.Check{Status: "checked", Issues: []domain.Issue{}, CheckedAt: domain.Now()}
	units := map[string]string{}
	for _, u := range k.Units {
		units[u.ID] = u.Text
	}
	for i, sec := range a.Sections {
		if len(sec.Citations) == 0 && strings.TrimSpace(sec.Body) != "" {
			check.Issues = append(check.Issues, domain.Issue{Code: "missing_citation", Index: i})
		}
		var support strings.Builder
		for _, cite := range sec.Citations {
			text, ok := units[cite]
			if !ok {
				check.Issues = append(check.Issues, domain.Issue{Code: "unknown_citation", Citation: cite, Index: i})
			}
			support.WriteString(text)
			support.WriteByte('\n')
		}
		known := numberSet(support.String())
		for n := range numberSet(sec.Title + "\n" + sec.Body + "\n" + sec.Notes) {
			if !known[n] {
				check.Issues = append(check.Issues, domain.Issue{Code: "unsupported_number", Detail: n, Index: i})
			}
		}
		for _, term := range k.Terms {
			for _, alias := range term.Deprecated {
				if alias != "" && strings.Contains(sec.Body+sec.Notes, alias) {
					check.Issues = append(check.Issues, domain.Issue{Code: "terminology_conflict", Detail: alias + " → " + term.Original, Index: i})
				}
			}
		}
		if a.Type == "B" && len([]rune(sec.Body)) > 550 {
			check.Issues = append(check.Issues, domain.Issue{Code: "layout_overflow", Index: i})
		}
	}
	for i, claim := range k.Claims {
		if !claim.Verified {
			check.Issues = append(check.Issues, domain.Issue{Code: "claim_review", Index: i})
		}
	}
	for _, issue := range k.Issues {
		if !issue.Resolved {
			check.Issues = append(check.Issues, issue)
		}
	}
	if a.Mode == "source-extract" {
		check.Issues = append(check.Issues, domain.Issue{Code: "extractive_draft"})
		if a.Language == "zh-TW" {
			check.Issues = append(check.Issues, domain.Issue{Code: "translation_pending"})
		}
	}
	if a.Type == "B" && a.Settings.Minutes > 0 && a.EstimatedMinutes > float64(a.Settings.Minutes) {
		check.Issues = append(check.Issues, domain.Issue{Code: "presentation_overtime"})
	}
	if len(check.Issues) > 0 {
		check.Status = "draft"
	}
	return check
}

// PresentationMinutes uses 130 English words or 260 Han characters per minute.
// Speaker notes provide the spoken text when available.
func PresentationMinutes(sections []domain.Section) float64 {
	han, words := 0, 0
	for _, section := range sections {
		text := section.Notes
		if strings.TrimSpace(text) == "" {
			text = section.Body
		}
		var other strings.Builder
		for _, r := range text {
			if unicode.Is(unicode.Han, r) {
				han++
				other.WriteByte(' ')
			} else {
				other.WriteRune(r)
			}
		}
		words += len(strings.Fields(other.String()))
	}
	return float64(words)/130 + float64(han)/260
}

func Excerpt(k domain.Knowledge, section int, depth string, lang string) domain.Section {
	limit := 2400
	switch depth {
	case "brief":
		limit = 900
	case "deep":
		limit = 6000
	}
	text := []string{}
	cites := []string{}
	seen := map[string]bool{}
	for _, c := range k.Claims {
		if c.Section == section {
			text = append(text, c.Text)
			for _, cite := range c.Citations {
				if !seen[cite] {
					cites = append(cites, cite)
					seen[cite] = true
				}
			}
		}
	}
	// Without key points, excerpt the pages that look like they discuss the
	// section (for example a related work section).
	if len(text) == 0 && section >= 0 && section < len(sectionPatterns) {
		for _, u := range k.Units {
			if len(cites) == 2 {
				break
			}
			if sectionPatterns[section].MatchString(u.Text) {
				text = append(text, Clip(strings.TrimSpace(u.Text), 900))
				cites = append(cites, u.ID)
			}
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
	return domain.Section{Title: Titles[lang][section], Body: Clip(body, limit), Citations: cites}
}

// Prompt asks for one section of an output, grounded in its evidence
// passages. See SectionPrompt for readers and decks.
func Prompt(k domain.Knowledge, sec domain.Section, lang, kind string, settings domain.OutputSettings, style domain.Style, known []string) string {
	r := Request{Knowledge: k, Section: sec, Index: -1, Lang: lang, Kind: kind, Settings: settings, Style: style, Known: known}
	passages, _ := r.Evidence()
	return r.Prompt(passages, nil)
}
