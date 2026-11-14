package experiments

import (
	"eduforge/internal/domain"
	"testing"
)

func criterion() domain.Plan {
	expected := 0.9
	p := domain.Plan{Metric: "accuracy", Expected: &expected, Tolerance: 0.01, Conditions: domain.Conditions{Task: "classification", Dataset: "fixture", Split: "test", Metric: "accuracy", Evaluation: "one-run"}}
	p.Confirmed = true
	p.ConfirmedHash = Hash(p)
	return p
}
func TestExecutionAndComparisonAreIndependent(t *testing.T) {
	p := criterion()
	actual, status, _, err := CompareCSV([]byte("accuracy\n0.905\n"), p, p.Conditions)
	if err != nil || actual == nil || status != "matched" {
		t.Fatalf("matching criterion: %v %s", err, status)
	}
	_, status, _, err = CompareCSV([]byte("accuracy\n0.70\n"), p, p.Conditions)
	if err != nil || status != "different" {
		t.Fatal("different result accepted")
	}
	c := p.Conditions
	c.Split = "other"
	_, status, _, err = CompareCSV([]byte("accuracy\n0.9\n"), p, c)
	if err != nil || status != "incomparable" {
		t.Fatal("incompatible result compared")
	}
}
func TestCriterionCannotChangeAfterResults(t *testing.T) {
	p := criterion()
	p.Tolerance = 1
	_, status, reason, err := CompareCSV([]byte("accuracy\n0.70\n"), p, p.Conditions)
	if err != nil || status != "pending" || reason != "criterion_not_frozen" {
		t.Fatal("changed criterion accepted")
	}
}
func TestAmbiguousAndNonFiniteCSV(t *testing.T) {
	p := criterion()
	if _, _, _, err := CompareCSV([]byte("accuracy\nNaN\n"), p, p.Conditions); err == nil {
		t.Fatal("NaN accepted")
	}
	_, status, reason, err := CompareCSV([]byte("accuracy\n0.9\n0.8\n"), p, p.Conditions)
	if err != nil || status != "pending" || reason != "select_single_metric_row" {
		t.Fatal("ambiguous rows compared")
	}
}
