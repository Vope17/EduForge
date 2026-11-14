package documents

import (
	"regexp"
	"strings"
)

// Some PDF fonts (no ToUnicode map) make pdftotext emit ASCII punctuation in
// place of ligature glyphs: "speci!c", "e"cient", "work#ow", "di$erent".
// The mapping differs per font, so each damaged word is repaired by trying the
// possible ligatures and keeping the one that spells a known English word.
// Words that cannot be confirmed are left unchanged.

// damagedWord matches letters, one placeholder, then at least one letter
// (word-initial "!rst" included).
var damagedWord = regexp.MustCompile(`[A-Za-z]*[!"#$][A-Za-z]+`)

// Tried in order; the first candidate that forms a known word wins.
var ligatureCandidates = []string{"fi", "ff", "ffi", "fl", "ffl"}

func repairWord(word string) string {
	i := strings.IndexAny(word, `!"#$`)
	before, after := word[:i], word[i+1:]
	for _, lig := range ligatureCandidates {
		if knownWord(before + lig + after) {
			return before + lig + after
		}
	}
	// Compounds missing from the list ("work" + "flow"): accept when the part
	// from the ligature on is a word and enough of a prefix precedes it.
	if len(before) >= 3 {
		for _, lig := range ligatureCandidates {
			if knownWord(lig + after) {
				return before + lig + after
			}
		}
	}
	return word
}

// RepairLigatures restores ligatures in text extracted from a PDF.
func RepairLigatures(text string) string {
	if !strings.ContainsAny(text, `!"#$`) {
		return text
	}
	return damagedWord.ReplaceAllStringFunc(text, repairWord)
}
