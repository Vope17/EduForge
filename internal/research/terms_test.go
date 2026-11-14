package research

import (
	"strings"
	"testing"

	"eduforge/internal/domain"
)

func termNames(terms []domain.Term) map[string]domain.Term {
	out := map[string]domain.Term{}
	for _, t := range terms {
		out[t.Original] = t
	}
	return out
}

func TestTermsComeFromDefinitionsNotCaptionsOrBoilerplate(t *testing.T) {
	page1 := strings.Join([]string{
		"ACM Reference Format:",
		"Haoran Xu. 2026. AIDA. https://doi.org/10.1145/3789240",
		"Figure 1: Typical RCA workflow for network devices.",
		"Challenge 1: Complexity and heterogeneity of historical emails slows diagnosis.",
		"Ensuring network stability requires root cause analysis (RCA) by experts.",
		"Others leverage retrieval-augmented generation (RAG) to recall similar cases.",
		"AIDA reduced the median Time to RCA (TRCA) in production.",
		"CPU: 4.8% busy, 95.2% idle",
		"DIAG TEST SUCCESS",
	}, "\n")
	page2 := strings.Join([]string{
		"We store each RCA case so the RAG retriever can rank RCA chains for operators.",
		"Operators compare the RAG output with the RCA report when they review incidents.",
		"References",
		"[1] Smith. Convolutional neural network (CNN) results. In Proc. IEEE.",
	}, "\n")
	terms := ExtractTerms([]domain.Unit{{ID: "s:p1", Text: page1}, {ID: "s:p2", Text: page2}}, "AIDA")
	got := termNames(terms)
	for _, want := range []string{"Root Cause Analysis", "Retrieval-augmented Generation", "Time to RCA"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("missing %q in %v", want, got)
		}
	}
	if got["Time to RCA"].Abbreviation != "TRCA" || !strings.Contains(got["Time to RCA"].Definition, "median Time to RCA") {
		t.Fatalf("abbreviation or example sentence wrong: %#v", got["Time to RCA"])
	}
	for name := range got {
		for _, bad := range []string{"Figure", "Challenge", "ACM", "https", "CPU", "DIAG", "TEST", "SUCCESS", "Convolutional", "CNN"} {
			if strings.Contains(name, bad) {
				t.Fatalf("noise term %q extracted: %v", name, got)
			}
		}
	}
}

func TestAbbreviationNeedsMatchingInitials(t *testing.T) {
	text := "The model reached high accuracy on the benchmark (MMLU) and a knowledge graph (KG) helps."
	got := termNames(ExtractTerms([]domain.Unit{{ID: "s:p1", Text: text}}, "x"))
	if _, ok := got["Knowledge Graph"]; !ok {
		t.Fatalf("knowledge graph missing: %v", got)
	}
	for name, term := range got {
		if term.Abbreviation == "MMLU" && name != "MMLU" {
			t.Fatalf("unrelated words taken as the long form of MMLU: %q", name)
		}
	}
}

func TestGlossarySectionsAreKept(t *testing.T) {
	text := "Centroid: arithmetic mean of feature vectors for one class.\nHoldout: fixed subset of examples reserved for evaluation."
	got := termNames(ExtractTerms([]domain.Unit{{ID: "s:p1", Text: text}}, "x"))
	if _, ok := got["Centroid"]; !ok {
		t.Fatalf("glossary entry dropped: %v", got)
	}
}
