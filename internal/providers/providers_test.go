package providers

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

func managerFixture(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	s, err := store.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return New(s, filepath.Join(root, "secrets")), s.LocalUser
}
func providerRecord(t *testing.T, m *Manager, owner string, p domain.Provider) domain.ProviderRef {
	t.Helper()
	r, err := m.Store.Create(context.Background(), domain.Record{Owner: owner, Kind: "provider", Data: domain.Encode(p)})
	if err != nil {
		t.Fatal(err)
	}
	return domain.ProviderRef{ID: r.ID, Provider: p}
}
func requestRecord(t *testing.T, m *Manager, owner string, refs []domain.ProviderRef, budget *float64) domain.Record {
	t.Helper()
	r, err := m.Store.Create(context.Background(), domain.Record{Owner: owner, Kind: "request", Data: domain.Encode(domain.Request{Status: "running", Providers: refs, AllowPaid: true, Budget: budget, Unlimited: budget == nil})})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestFailoverUsesNextProviderAndPreservesRequestSettings(t *testing.T) {
	m, owner := managerFixture(t)
	ctx := context.Background()
	calls := []string{}
	first := providerRecord(t, m, owner, domain.Provider{Kind: "codex", Enabled: true, Model: "configured-a"})
	second := providerRecord(t, m, owner, domain.Provider{Kind: "claude-cli", Enabled: true, Model: "configured-b"})
	req := requestRecord(t, m, owner, []domain.ProviderRef{first, second}, nil)
	m.Transports["codex"] = func(context.Context, domain.Provider, string, string) (string, Usage, error) {
		calls = append(calls, "first")
		return "", Usage{}, errors.New("quota_exhausted")
	}
	m.Transports["claude-cli"] = func(context.Context, domain.Provider, string, string) (string, Usage, error) {
		calls = append(calls, "second")
		return `{"title":"測試","body":"來源原文","notes":"講稿","citations":["s:p1"]}`, Usage{}, nil
	}
	got, err := m.Generate(ctx, owner, req.ID, "Generate zh-TW from fixed evidence", domain.Section{})
	if err != nil || got.Provider != second.ID || len(calls) != 2 || got.Section.Body != "來源原文" {
		t.Fatalf("failover failed: %#v %v", got, err)
	}
	attempts, err := m.Store.List(ctx, owner, store.Filter{Kind: "attempt"})
	if err != nil || len(attempts) != 2 {
		t.Fatal("attempt provenance missing")
	}
}
func TestMissingCredentialReleasesReservation(t *testing.T) {
	m, owner := managerFixture(t)
	price, cap := 1.0, 1.0
	ref := providerRecord(t, m, owner, domain.Provider{Kind: "openai", Enabled: true, Paid: true, Model: "configured", MaxOutput: 128, InputPrice: &price, OutputPrice: &price})
	req := requestRecord(t, m, owner, []domain.ProviderRef{ref}, &cap)
	if _, err := m.Generate(context.Background(), owner, req.ID, "test", domain.Section{}); !errors.Is(err, domain.ErrPaused) {
		t.Fatal("expected the unavailable source to pause")
	}
	row, err := m.Store.Get(context.Background(), owner, req.ID, "request")
	if err != nil {
		t.Fatal(err)
	}
	state, _ := domain.Decode[domain.Request](row.Data)
	if state.Spent != 0 {
		t.Fatal("a locally rejected call retained a spending reservation")
	}
}

func TestLegacySpendingLimitsDoNotBlockGeneration(t *testing.T) {
	m, owner := managerFixture(t)
	cap := 0.0
	ref := providerRecord(t, m, owner, domain.Provider{Kind: "openai", Enabled: true, Paid: true, Model: "configured"})
	req := requestRecord(t, m, owner, []domain.ProviderRef{ref}, &cap)
	m.Transports["openai"] = func(context.Context, domain.Provider, string, string) (string, Usage, error) {
		return `{"title":"Test","body":"Source","citations":[]}`, Usage{Input: 10, Output: 5}, nil
	}
	if _, err := m.Generate(context.Background(), owner, req.ID, "prompt", domain.Section{}); err != nil {
		t.Fatal(err)
	}
}
