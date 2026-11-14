package app

import (
	"context"
	"testing"

	"eduforge/internal/domain"
	"eduforge/internal/providers"
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

func mustGet(t *testing.T, a *App, owner, id string) domain.Record {
	t.Helper()
	r, err := a.Store.Get(context.Background(), owner, id, "")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func storeFilter(kind, lesson string) store.Filter { return store.Filter{Kind: kind, Lesson: lesson} }
