package domain

import "testing"

func TestCitationMarkersAreRemovedFromProse(t *testing.T) {
	id := "1c379de376f7d26460d3a7966799d12e"
	cases := map[string]string{
		"佔故障解決時間的大部分。〔" + id + ":p1〕〔" + id + ":p2〕":   "佔故障解決時間的大部分。",
		"縮短時間（TRCA）。【p1】【p2】":                          "縮短時間（TRCA）。",
		"see results [p3, p4].":                        "see results.",
		"bare " + id + ":s2 id":                        "bare  id",
		"keep (1) equation and [CLS] token and (TRCA)": "keep (1) equation and [CLS] token and (TRCA)",
	}
	for in, want := range cases {
		if got := StripCitationMarkers(in); got != want {
			t.Errorf("StripCitationMarkers(%q) = %q, want %q", in, got, want)
		}
	}
}
