package documents

import "testing"

func TestJoinWordsRejoinsLineBreakHyphens(t *testing.T) {
	cases := map[string][]string{
		"showing that TRCA dominates failure":          {"showing", "that", "TRCA", "domi-", "nates", "failure"},
		"over 5-year period (27%":                      {"over", "5-year", "pe-", "riod", "(27%"},
		"dotted-style links are non-causal, introduce": {"dotted-style", "links", "are", "non-", "causal,", "introduce"},
		"Multi-Vendor Device":                          {"Multi-", "Vendor", "Device"},
		"pre- and post-processing":                     {"pre-", "and", "post-processing"},
		"naive RAG retrieval.":                         {"naive", "RAG", "re-", "trieval."},
	}
	for want, words := range cases {
		if got := joinWords(words); got != want {
			t.Errorf("joinWords(%q) = %q, want %q", words, got, want)
		}
	}
}

func TestJoinHyphenatedLines(t *testing.T) {
	in := "TRCA domi-\nnates failure resolution\nnon-\ncausal links\nStep 3 -\n4"
	want := "TRCA dominates failure resolution\nnon-causal links\nStep 3 -\n4"
	if got := JoinHyphenatedLines(in); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestPDFTitle(t *testing.T) {
	info := "Title:           AIDA: Accelerating Root Cause Analysis for Multi-Vendor Device Failures\nAuthor: x\n"
	if got := pdfTitle(info); got != "AIDA: Accelerating Root Cause Analysis for Multi-Vendor Device Failures" {
		t.Fatalf("got %q", got)
	}
	for _, junk := range []string{"Title: paper.pdf", "Title: Microsoft Word - draft.docx", "Title: untitled document", "Title: main", "Title:   "} {
		if got := pdfTitle(junk); got != "" {
			t.Errorf("pdfTitle(%q) = %q, want empty", junk, got)
		}
	}
}
