package documents

import "testing"

func TestLigaturePlaceholdersAreRestoredFromKnownWords(t *testing.T) {
	cases := map[string]string{
		"speci!c con!dence Con$dence":       "specific confidence Confidence",
		"e\"cient o\"er o\"line di$erent":   "efficient offer offline different",
		"in#uence work#ow e#ciency #apping": "influence workflow efficiency flapping",
		"E\"ectiveness Re$nement !rst":      "Effectiveness Refinement first",
	}
	for in, want := range cases {
		if got := RepairLigatures(in); got != want {
			t.Errorf("RepairLigatures(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLigatureRepairLeavesRealPunctuationAlone(t *testing.T) {
	for _, s := range []string{`Wow!Great "quoted" C# AT&T $100 x!=y mce_test`, `say "hello" and #hashtag`} {
		if got := RepairLigatures(s); got != s {
			t.Errorf("RepairLigatures(%q) changed text to %q", s, got)
		}
	}
}
