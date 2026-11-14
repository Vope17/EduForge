package style

import (
	"eduforge/internal/domain"
	"testing"
)

func TestResolvePriorityAndExplicitClear(t *testing.T) {
	got, err := Resolve("B", domain.Style{"tone": "academic", "accent": "111111"}, domain.Style{"accent": "222222"}, domain.Style{"accent": "333333"}, domain.Style{"accent": "444444", "tone": nil})
	if err != nil {
		t.Fatal(err)
	}
	if got["accent"] != "444444" || got["tone"] != nil {
		t.Fatalf("wrong resolution: %#v", got)
	}
}
func TestStyleCannotChangeFactsOrLanguage(t *testing.T) {
	for _, key := range []string{"language", "commands", "expected", "metric", "knowledge_id"} {
		if _, err := Resolve("C", nil, nil, nil, domain.Style{key: "changed"}); err == nil {
			t.Fatalf("accepted forbidden style field %s", key)
		}
	}
}
func TestSelectedBranchesOnly(t *testing.T) {
	snapshot, err := Snapshot([]string{"A"}, domain.ProfileVersion{Common: domain.Style{"tone": "academic"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Resolved) != 1 || snapshot.Resolved["B"] != nil || snapshot.Resolved["C"] != nil {
		t.Fatal("unselected branch resolved")
	}
}
