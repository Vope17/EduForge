package research

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"eduforge/internal/domain"
)

// Term extraction favours how lessons actually introduce vocabulary:
//  1. "long form (ABBR)" where the long form's initials spell the abbreviation,
//  2. short "Term: definition" lines (not captions, headings or URLs),
//  3. undefined acronyms that recur often enough to matter.
// Text after the references heading is ignored.

const (
	maxTerms    = 25
	maxGlossary = 15
)

var (
	referencesHeading = regexp.MustCompile(`(?m)^\s*(References|REFERENCES|Bibliography|BIBLIOGRAPHY|參考文獻)\s*$`)
	hyphenBreak       = regexp.MustCompile(`([A-Za-z])-\n([a-z])`)
	abbrDefinition    = regexp.MustCompile(`((?:[A-Za-z][A-Za-z0-9'’-]*\s+){0,8}[A-Za-z][A-Za-z0-9'’-]*)\s*\(([A-Z][A-Za-z0-9]{1,9}?)(s?)\)`)
	glossaryLine      = regexp.MustCompile(`(?m)^\s*([A-Z][A-Za-z0-9-]*(?:\s+[A-Za-z0-9-]+){0,3})\s*[:：]\s*(\S[^\n]{10,250})`)
	acronymGlossLine  = regexp.MustCompile(`^\s*([A-Z][A-Z0-9]{1,9})\s*[:：]\s*(\S.{5,200})$`)
	acronym           = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,9}s?\b`)
	notGlossary       = regexp.MustCompile(`(?i)^(figure|fig|table|tab|challenge|observation|step|case|section|appendix|example|note|lemma|theorem|definition|proof|algorithm|equation|eq|round|stage|phase|question|answer|input|output|keywords?|ccs concepts|acm reference format|https?|doi|arxiv|email|e-mail|abstract|introduction|conclusion|acknowledg\w*|contributions?|limitations?|goal|insight|solution|problem|result|summary|q\d*|a\d*|rq\d*)\b`)
	sentenceSplit     = regexp.MustCompile(`[.!?。！？]\s`)
)

var minorWords = map[string]bool{"of": true, "for": true, "the": true, "and": true, "to": true, "with": true, "in": true, "on": true, "a": true, "an": true, "by": true, "via": true, "from": true}

// Acronyms that are bibliographic or locational boilerplate, not concepts.
var acronymStoplist = map[string]bool{
	"ACM": true, "IEEE": true, "USENIX": true, "CCS": true, "DOI": true, "ISBN": true, "ISSN": true, "URL": true, "PDF": true,
	"USA": true, "UK": true, "EU": true, "CA": true, "CO": true, "NY": true, "ID": true, "IDS": true, "OK": true, "NA": true, "TBD": true,
	"II": true, "III": true, "IV": true, "VI": true, "VII": true, "VIII": true, "IX": true, "XI": true, "XII": true,
	"AM": true, "PM": true, "UTC": true, "GMT": true, "ET": true, "AL": true, "FIG": true, "EQ": true, "SIGCOMM": true,
}

type termCandidate struct {
	term  domain.Term
	count int
	rank  int // 0 = defined abbreviation, 1 = glossary line, 2 = bare acronym
}

// readingText returns the units' text without the references section, with
// hyphenated line breaks rejoined.
func readingText(units []domain.Unit) []domain.Unit {
	out := []domain.Unit{}
	for _, u := range units {
		text := hyphenBreak.ReplaceAllString(u.Text, "$1$2")
		if loc := referencesHeading.FindStringIndex(text); loc != nil {
			u.Text = text[:loc[0]]
			out = append(out, u)
			break
		}
		u.Text = text
		out = append(out, u)
	}
	return out
}

// tokenLetters is what a word contributes to an abbreviation: an existing
// acronym contributes all its letters (Time to RCA → TRCA), others their initial.
func tokenLetters(word string) string {
	letters := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, word)
	if letters == "" {
		return ""
	}
	if len(letters) >= 2 && strings.ToUpper(letters) == letters && unicode.IsLetter(rune(letters[0])) {
		return letters
	}
	return string([]rune(letters)[0])
}

// longFormFor finds the shortest run of trailing words whose initials spell abbr.
func longFormFor(words []string, abbr string) string {
	target := strings.ToLower(abbr)
	for n := 1; n <= len(words); n++ {
		phrase := words[len(words)-n:]
		if minorWords[strings.ToLower(phrase[0])] {
			continue
		}
		for _, skipMinor := range []bool{true, false} {
			var letters strings.Builder
			for _, w := range phrase {
				for _, part := range strings.Split(w, "-") {
					if skipMinor && minorWords[strings.ToLower(part)] {
						continue
					}
					letters.WriteString(tokenLetters(part))
				}
			}
			if strings.ToLower(letters.String()) == target {
				return strings.Join(phrase, " ")
			}
		}
	}
	return ""
}

func sentenceAround(text string, start, end int) string {
	from := 0
	for _, m := range sentenceSplit.FindAllStringIndex(text[:start], -1) {
		from = m[1]
	}
	to := len(text)
	if m := sentenceSplit.FindStringIndex(text[end:]); m != nil {
		to = end + m[0] + 1
	}
	return Clip(strings.Join(strings.Fields(text[from:to]), " "), 240)
}

// proseScore counts lowercase words of three or more letters: high for
// sentences, low for figure labels, headings and key/value lines.
func proseScore(text string) int {
	n := 0
	for _, w := range strings.Fields(text) {
		if r := []rune(w); len(r) > 2 && unicode.IsLower(r[0]) {
			n++
		}
	}
	return n
}

// proseDefinition accepts "Term: definition" only when the term is a short
// name without digits and the definition reads as a sentence, which rules
// out captions, headings, URLs and key/value lines inside figures.
func proseDefinition(name, definition string) bool {
	if notGlossary.MatchString(name) || strings.ContainsAny(name, "0123456789") || strings.HasPrefix(definition, "//") {
		return false
	}
	words := strings.Fields(definition)
	if len(words) < 5 {
		return false
	}
	lower, digits, runes := 0, 0, 0
	for _, w := range words {
		if r := []rune(w); unicode.IsLower(r[0]) && len(r) > 2 {
			lower++
		}
	}
	for _, r := range definition {
		runes++
		if unicode.IsDigit(r) {
			digits++
		}
	}
	return lower >= 3 && digits*10 < runes
}

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if i > 0 && minorWords[strings.ToLower(w)] {
			words[i] = strings.ToLower(w)
			continue
		}
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// ExtractTerms builds a short, ranked glossary from the research units.
func ExtractTerms(units []domain.Unit, context string) []domain.Term {
	units = readingText(units)
	byKey := map[string]*termCandidate{}
	add := func(key string, c termCandidate) {
		key = strings.ToLower(key)
		if existing, ok := byKey[key]; ok {
			existing.count++
			// Keep the occurrence that reads most like prose as the example.
			if proseScore(c.term.Definition) > proseScore(existing.term.Definition) {
				existing.term.Definition = c.term.Definition
				existing.term.Citations = c.term.Citations
			}
			return
		}
		c.count = 1
		byKey[key] = &c
	}

	acronymCounts := map[string]int{}
	acronymPages := map[string]map[string]bool{}
	acronymUnit := map[string]string{}
	acronymGloss := map[string]string{}
	for _, u := range units {
		flat := strings.Join(strings.Fields(u.Text), " ")
		for _, m := range abbrDefinition.FindAllStringSubmatchIndex(flat, -1) {
			abbr := flat[m[4]:m[5]]
			if acronymStoplist[strings.ToUpper(abbr)] || len(abbr) < 2 {
				continue
			}
			long := longFormFor(strings.Fields(flat[m[2]:m[3]]), abbr)
			if long == "" || strings.EqualFold(long, abbr) {
				continue
			}
			add(abbr, termCandidate{rank: 0, term: domain.Term{
				Original: titleCase(long), English: titleCase(long), Abbreviation: abbr,
				Definition: sentenceAround(flat, m[0], m[1]), Citations: []string{u.ID}, Context: context,
			}})
		}
		for _, m := range glossaryLine.FindAllStringSubmatch(u.Text, -1) {
			name := strings.Join(strings.Fields(m[1]), " ")
			definition := strings.Join(strings.Fields(m[2]), " ")
			if !proseDefinition(name, definition) {
				continue
			}
			add(name, termCandidate{rank: 1, term: domain.Term{
				Original: name, English: name, Definition: Clip(definition, 240), Citations: []string{u.ID}, Context: context,
			}})
		}
		for _, line := range strings.Split(u.Text, "\n") {
			if m := acronymGlossLine.FindStringSubmatch(line); m != nil {
				if _, ok := acronymGloss[m[1]]; !ok {
					acronymGloss[m[1]] = strings.Join(strings.Fields(m[2]), " ")
				}
			}
			// Only count acronyms used in sentences, not labels inside figures.
			if proseScore(line) < 4 {
				continue
			}
			for _, a := range acronym.FindAllString(line, -1) {
				a = strings.TrimSuffix(a, "s")
				if len(a) < 2 || acronymStoplist[a] {
					continue
				}
				acronymCounts[a]++
				if acronymPages[a] == nil {
					acronymPages[a] = map[string]bool{}
					acronymUnit[a] = u.ID
				}
				acronymPages[a][u.ID] = true
			}
		}
	}
	for a, n := range acronymCounts {
		if c, ok := byKey[strings.ToLower(a)]; ok {
			c.count += n
			continue
		}
		if n >= 3 && len(acronymPages[a]) >= 2 {
			byKey[strings.ToLower(a)] = &termCandidate{rank: 2, count: n, term: domain.Term{
				Original: a, English: a, Abbreviation: a, Definition: Clip(acronymGloss[a], 240), Citations: []string{acronymUnit[a]}, Context: context,
			}}
		}
	}

	// Rank glossary-line terms by how often the name recurs in the document:
	// real vocabulary recurs, one-off labels copied from figures do not.
	var all strings.Builder
	for _, u := range units {
		all.WriteString(strings.ToLower(strings.Join(strings.Fields(u.Text), " ")))
		all.WriteByte(' ')
	}
	full := all.String()
	for _, c := range byKey {
		if c.rank == 1 {
			c.count = strings.Count(full, strings.ToLower(c.term.Original))
		}
	}
	candidates := make([]*termCandidate, 0, len(byKey))
	for _, c := range byKey {
		candidates = append(candidates, c)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		if candidates[i].count != candidates[j].count {
			return candidates[i].count > candidates[j].count
		}
		return candidates[i].term.Original < candidates[j].term.Original
	})
	terms := []domain.Term{}
	glossary := 0
	for _, c := range candidates {
		if len(terms) == maxTerms {
			break
		}
		if c.rank == 1 {
			if glossary == maxGlossary {
				continue
			}
			glossary++
		}
		terms = append(terms, c.term)
	}
	return terms
}
