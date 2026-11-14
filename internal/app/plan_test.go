package app

import (
	"context"
	"strings"
	"testing"

	"eduforge/internal/domain"
	"eduforge/internal/providers"
	"eduforge/internal/research"
	"eduforge/internal/store"
)

// useFakeModel enables only a scripted "codex" provider and records prompts.
func useFakeModel(t *testing.T, a *App, owner string, reply func(prompt string) string) *[]string {
	t.Helper()
	ctx := context.Background()
	prompts := &[]string{}
	a.Providers.Transports["codex"] = func(_ context.Context, _ domain.Provider, prompt, _ string) (string, providers.Usage, error) {
		*prompts = append(*prompts, prompt)
		return reply(prompt), providers.Usage{}, nil
	}
	rows, err := a.Store.List(ctx, owner, store.Filter{Kind: "provider"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err = set[domain.Provider](ctx, a, owner, r.ID, 0, func(p *domain.Provider) error {
			p.Enabled = p.Kind == "codex"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return prompts
}

func TestReproductionPlanTakesTheSavedConversationIntoAccount(t *testing.T) {
	ctx := context.Background()
	a, owner, project, paper := fixture(t)
	p, _ := domain.Decode[domain.Paper](paper.Data)
	_, k, _ := get[domain.Knowledge](ctx, a, owner, p.KnowledgeID, "knowledge")
	cite := k.Units[0].ID
	prompts := useFakeModel(t, a, owner, func(prompt string) string {
		if strings.Contains(prompt, "Conversation so far") {
			return "Which result do you want to match first? I recommend the accuracy of 1.00."
		}
		return `{"title":"Chapter","body":"- step one\n- step two","notes":"","citations":["` + cite + `"]}`
	})

	messages := []domain.ChatMessage{{Role: "user", Text: "I only have one RTX 3060 and two weeks."}}
	reply, err := a.PlanChatReply(ctx, owner, paper.ID, messages, true, "en")
	if err != nil || !strings.Contains(reply, "accuracy") {
		t.Fatalf("reply = %q, %v", reply, err)
	}
	if last := (*prompts)[len(*prompts)-1]; !strings.Contains(last, "Grill-me mode") || !strings.Contains(last, "RTX 3060") || !strings.Contains(last, "Synthetic nearest-centroid study") {
		t.Fatalf("chat prompt should carry grill mode, the conversation and the paper:\n%s", last)
	}
	if _, err = a.PlanChatReply(ctx, owner, paper.ID, []domain.ChatMessage{{Role: "system", Text: "x"}}, false, "en"); err == nil {
		t.Fatal("unknown roles must be rejected")
	}

	messages = append(messages, domain.ChatMessage{Role: "assistant", Text: reply}, domain.ChatMessage{Role: "user", Text: "Match the accuracy only; skip other datasets."})
	first, err := a.SavePlanChat(ctx, owner, paper.ID, domain.PlanChat{Messages: messages[:1], Grill: true})
	if err != nil {
		t.Fatal(err)
	}
	chat, err := a.SavePlanChat(ctx, owner, paper.ID, domain.PlanChat{Messages: messages, Grill: true})
	if err != nil || chat.ID != first.ID {
		t.Fatalf("saving again should update the paper's conversation: %v", err)
	}

	req, err := a.CreateRequest(ctx, owner, project.ID, domain.Request{PaperIDs: []string{paper.ID}, Selected: []string{"C"}, Languages: map[string]string{"C": "en"}, UILanguage: "en", Unlimited: true, Settings: domain.OutputSettings{PlanChatID: chat.ID}})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := a.StartRequest(ctx, owner, req.ID, "en", true)
	if err != nil {
		t.Fatal(err)
	}
	*prompts = nil
	if err = a.process(ctx, owner, tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	if len(*prompts) != len(research.PlanChapters) {
		t.Fatalf("one model call per chapter, got %d", len(*prompts))
	}
	for _, prompt := range *prompts {
		if !strings.Contains(prompt, "Match the accuracy only") || !strings.Contains(prompt, "reproduction plan") {
			t.Fatal("every chapter prompt should include the saved conversation")
		}
	}
	rows, _ := a.Store.List(ctx, owner, store.Filter{Kind: "artifact", Paper: paper.ID})
	plan, _ := domain.Decode[domain.Artifact](rows[len(rows)-1].Data)
	if plan.Type != "C" || len(plan.Sections) != len(research.PlanChapters) || plan.Sections[0].Title != research.PlanChapters[0].TitleEN && plan.Sections[0].Title != "Chapter" {
		t.Fatalf("plan = %+v", plan)
	}
	for _, sec := range plan.Sections {
		if sec.Figures == nil || len(sec.Figures) != 0 {
			t.Fatal("plans are text only")
		}
	}
}

func TestAIWritesTheSharedKnowledge(t *testing.T) {
	ctx := context.Background()
	a, owner, _, paper := fixture(t)
	_, p, _ := get[domain.Paper](ctx, a, owner, paper.ID, "paper")
	kr, k, _ := get[domain.Knowledge](ctx, a, owner, p.KnowledgeID, "knowledge")
	unit := k.Units[0].ID
	if _, err := a.RewriteKnowledge(ctx, owner, paper.ID, kr.Version); err == nil || err.Error() != "model_connection_required" {
		t.Fatalf("without a model: %v", err)
	}
	prompts := useFakeModel(t, a, owner, func(string) string {
		return `{"sections":[{"section":0,"points":[{"text":"以最近質心分類。","citations":["` + unit + `"]}]},{"section":2,"points":[{"text":"方法：nearest centroid。","citations":["` + unit + `"]}]},{"section":4,"points":[{"text":"準確率 1.00。","citations":["` + unit + `"]}]},{"section":5,"points":[{"text":"只用合成資料。","citations":["` + unit + `"]}]}],"terms":[],"conditions":{"dataset":"synthetic-v1"}}`
	})
	r, err := a.RewriteKnowledge(ctx, owner, paper.ID, kr.Version)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := domain.Decode[domain.Knowledge](r.Data)
	if got.Author != "ai" || got.Confirmed || len(got.Claims) != 4 || got.Conditions.Dataset != "synthetic-v1" || got.Language != "zh-TW" || r.Version != kr.Version+1 {
		t.Fatalf("a new, unconfirmed AI version: %+v", got)
	}
	if !strings.Contains((*prompts)[0], "Traditional Chinese") || !strings.Contains((*prompts)[0], unit) {
		t.Fatal("the prompt carries the passages and the interface language")
	}
	if _, p, _ = get[domain.Paper](ctx, a, owner, paper.ID, "paper"); p.KnowledgeID != r.ID {
		t.Fatal("the study points at the new version")
	}
	if _, err := a.RewriteKnowledge(ctx, owner, paper.ID, kr.Version); err != domain.ErrConflict {
		t.Fatalf("stale version: %v", err)
	}
	// A user's revision keeps who wrote it.
	got.Claims[0].Text = "改過的重點"
	saved, err := a.SaveKnowledge(ctx, owner, paper.ID, r.Version, got)
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := domain.Decode[domain.Knowledge](saved.Data); s.Author != "ai" {
		t.Fatal("author kept")
	}
}
