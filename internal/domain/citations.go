package domain

import (
	"regexp"
	"strings"
)

// Citations live in Section.Citations and are rendered by the exporters, so
// any citation markers a model writes into prose are noise: internal unit IDs
// such as 〔1c37…e:p1〕 or short forms such as 【p2】 / [p3, p4].
var (
	citeRef        = `(?:[0-9a-f]{32}:)?(?:p|s|page|slide)\s*\d+`
	bracketedCites = regexp.MustCompile(`[〔【\[（(]\s*` + citeRef + `(?:\s*[,，、;；]\s*` + citeRef + `)*\s*[〕】\]）)]`)
	bareCiteIDs    = regexp.MustCompile(`\b[0-9a-f]{32}:[ps]\d+\b`)
	spaceBeforeEnd = regexp.MustCompile(`[ \t]+([。，、；：！？.,;:!?)）])`)
	trailingSpace  = regexp.MustCompile(`[ \t]+(\n|$)`)
)

// StripCitationMarkers removes inline citation markers from prose.
func StripCitationMarkers(text string) string {
	if !strings.ContainsAny(text, "〔【[（(") && !bareCiteIDs.MatchString(text) {
		return text
	}
	text = bracketedCites.ReplaceAllString(text, "")
	text = bareCiteIDs.ReplaceAllString(text, "")
	text = spaceBeforeEnd.ReplaceAllString(text, "$1")
	return trailingSpace.ReplaceAllString(text, "$1")
}
