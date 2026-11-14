package research

import (
	"eduforge/internal/domain"
	"strings"
	"testing"
)

func TestPresentationTimeUsesNotesAndCountsChinese(t *testing.T) {
	sections := []domain.Section{{Body: strings.Repeat("word ", 130)}, {Body: "unused", Notes: strings.Repeat("中", 260)}}
	if got := PresentationMinutes(sections); got != 2 {
		t.Fatalf("speaking time = %v, want 2 minutes", got)
	}
	check := Validate(domain.Artifact{Type: "B", EstimatedMinutes: 2, Settings: domain.OutputSettings{Minutes: 1}}, domain.Knowledge{})
	if len(check.Issues) != 1 || check.Issues[0].Code != "presentation_overtime" {
		t.Fatal("overtime was not surfaced")
	}
}

func TestWrongNumbersReferencesAndTermsStayDraft(t *testing.T) {
	k := domain.Knowledge{Units: []domain.Unit{{ID: "source:p1", Text: "Accuracy is 0.91 using CanonicalMethod."}}, Terms: []domain.Term{{Original: "CanonicalMethod", Deprecated: []string{"WrongMethod"}}}}
	a := domain.Artifact{Type: "A", Language: "en", Mode: "model", Sections: []domain.Section{{Body: "WrongMethod achieved 0.99.", Citations: []string{"source:p1"}}, {Body: "Unknown", Citations: []string{"unknown:p3"}}}}
	result := Validate(a, k)
	found := map[string]bool{}
	for _, issue := range result.Issues {
		found[issue.Code] = true
	}
	if result.Status != "draft" || !found["unsupported_number"] || !found["unknown_citation"] || !found["terminology_conflict"] {
		t.Fatalf("issues missing: %#v", result)
	}
}
func TestDesignReferencesNeverEnterKnowledge(t *testing.T) {
	sources := []domain.Record{{ID: "research", Data: domain.Encode(domain.Source{Role: "research", Parsed: &domain.Parsed{Units: []domain.Unit{{ID: "research:p1", Text: "Method: nearest centroid\nResults: 0.91"}}}})}, {ID: "design", Data: domain.Encode(domain.Source{Role: "design", Parsed: &domain.Parsed{Units: []domain.Unit{{ID: "design:s1", Text: "Invented claim: 999"}}}})}}
	k := Extract("A study", sources)
	if len(k.Units) != 1 || k.Units[0].ID != "research:p1" {
		t.Fatal("design content contaminated research")
	}
}
func TestResolvedKnowledgeIssuesDoNotKeepArtifactsInDraft(t *testing.T) {
	k := domain.Knowledge{Issues: []domain.Issue{{Code: "formula_review", Citation: "u:p1", Resolved: true}}}
	if check := Validate(domain.Artifact{Type: "A", Mode: "model"}, k); check.Status != "checked" || len(check.Issues) != 0 {
		t.Fatalf("resolved issue still reported: %#v", check)
	}
	k.Issues = append(k.Issues, domain.Issue{Code: "formula_review", Citation: "u:p2"})
	if check := Validate(domain.Artifact{Type: "A", Mode: "model"}, k); check.Status != "draft" || len(check.Issues) != 1 {
		t.Fatalf("unresolved issue missing: %#v", check)
	}
}

func TestRuleBasedKnowledgeSkipsPagesWithoutText(t *testing.T) {
	src := domain.Source{Name: "scan.pdf", Role: "research", Parsed: &domain.Parsed{Units: []domain.Unit{
		{ID: "a:p1", Kind: "page", Locator: 1, Mode: "vision"},
		{ID: "a:p2", Kind: "page", Locator: 2, Mode: "text", Text: "Definition: speed is distance travelled per unit time."},
	}}}
	k := Extract("Speed", []domain.Record{{ID: "a", Kind: "source", Data: domain.Encode(src)}})
	for _, c := range k.Claims {
		if strings.TrimSpace(c.Text) == "" {
			t.Fatalf("an empty point from a page with no text: %+v", c)
		}
	}
	scanOnly := domain.Source{Name: "scan.pdf", Role: "research", Parsed: &domain.Parsed{Units: []domain.Unit{{ID: "b:p1", Kind: "page", Locator: 1, Mode: "vision"}}}}
	k = Extract("Scan", []domain.Record{{ID: "b", Kind: "source", Data: domain.Encode(scanOnly)}})
	if len(k.Claims) != 0 || len(k.Units) != 1 {
		t.Fatalf("a scan alone has pages but no rule-based points until a model reads it: %d claims", len(k.Claims))
	}
}
