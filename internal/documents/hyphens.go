package documents

import (
	"regexp"
	"strings"
	"unicode"
)

// Words a line-break hyphen is kept apart from ("pre- and post-processing").
var hyphenKeepApart = map[string]bool{"and": true, "or": true, "to": true, "nor": true}

// mergeHyphen joins a word split across lines ("domi-" + "nates"). A known
// word drops the hyphen; otherwise the hyphen is real ("non-causal",
// "Multi-Vendor") and kept. ok is false when b does not continue a.
func mergeHyphen(a, b string) (string, bool) {
	ra, rb := []rune(a), []rune(b)
	if len(ra) < 2 || len(rb) == 0 || ra[len(ra)-1] != '-' || !unicode.IsLetter(ra[len(ra)-2]) || !unicode.IsLetter(rb[0]) {
		return "", false
	}
	if unicode.IsUpper(rb[0]) {
		return a + b, true
	}
	if hyphenKeepApart[strings.ToLower(strings.TrimRightFunc(b, func(r rune) bool { return !unicode.IsLetter(r) }))] {
		return "", false
	}
	joined := RepairLigatures(a[:len(a)-1] + b)
	core := strings.TrimFunc(joined, func(r rune) bool { return !unicode.IsLetter(r) })
	if knownWord(core) && !compoundPrefix(a[:len(a)-1], b) {
		return joined, true
	}
	return a + b, true
}

// Prefixes that usually form hyphenated compounds ("non-causal",
// "self-attention") rather than syllable breaks.
var compoundPrefixes = map[string]bool{"non": true, "self": true, "multi": true, "cross": true, "semi": true, "anti": true, "well": true}

func compoundPrefix(prefix, rest string) bool {
	prefix = strings.ToLower(strings.TrimLeftFunc(prefix, func(r rune) bool { return !unicode.IsLetter(r) }))
	rest = strings.TrimRightFunc(rest, func(r rune) bool { return !unicode.IsLetter(r) })
	return compoundPrefixes[prefix] && len(rest) >= 3 && knownWord(rest)
}

// joinWords joins the words of a text block, rejoining words that a line
// break split with a hyphen.
func joinWords(words []string) string {
	out := []string{}
	for _, w := range words {
		if n := len(out); n > 0 {
			if merged, ok := mergeHyphen(out[n-1], w); ok {
				out[n-1] = merged
				continue
			}
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}

var (
	lineHyphen    = regexp.MustCompile(`(\S+-)\n(\S+)`)
	fileNameTitle = regexp.MustCompile(`\.(pdf|docx?|tex|dvi|pptx?)$`)
)

// JoinHyphenatedLines rejoins words split across lines of page text.
func JoinHyphenatedLines(text string) string {
	return lineHyphen.ReplaceAllStringFunc(text, func(m string) string {
		a, b, _ := strings.Cut(m, "\n")
		if merged, ok := mergeHyphen(a, b); ok {
			return merged
		}
		return m
	})
}

// pdfTitle reads the Title line of pdfinfo output, ignoring placeholder or
// file-name titles that tools often leave there.
func pdfTitle(info string) string {
	for _, line := range strings.Split(info, "\n") {
		value, ok := strings.CutPrefix(line, "Title:")
		if !ok {
			continue
		}
		title := strings.Join(strings.Fields(value), " ")
		lower := strings.ToLower(title)
		switch {
		case len([]rune(title)) < 8, !strings.Contains(title, " "),
			strings.HasPrefix(lower, "microsoft "), strings.HasPrefix(lower, "untitled"),
			fileNameTitle.MatchString(lower):
			return ""
		}
		return title
	}
	return ""
}
