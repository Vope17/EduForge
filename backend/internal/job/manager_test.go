package job

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"eduforge/backend/internal/artifact"
	"eduforge/backend/internal/generate"
	"eduforge/backend/internal/llm"
	"eduforge/backend/internal/store"
	"eduforge/backend/internal/template"
)

type mockProvider struct {
	calls   atomic.Int32
	started chan struct{}
}

func (p *mockProvider) Ready() bool { return true }
func (p *mockProvider) Generate(ctx context.Context, r llm.Request) (json.RawMessage, error) {
	var input struct {
		Template *template.Spec `json:"confirmed_template"`
	}
	if err := json.Unmarshal(r.Data, &input); err != nil {
		return nil, err
	}
	page := artifact.Block{Kind: "slide", Layout: "title_body", Text: "", Notes: "Ask why", Children: []artifact.Block{{Kind: "heading", Text: "Water"}, {Kind: "paragraph", Text: "Water changes state"}}}
	if input.Template == nil {
		return json.Marshal(map[string]any{"language": "en", "palette": "forest", "font": "sans", "layout": "title_body", "tone": "friendly", "density": "balanced", "example": []artifact.Block{page}})
	}
	call := p.calls.Add(1)
	if call == 1 {
		close(p.started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if call == 2 {
		return nil, llm.ErrSubscriptionLimit
	}
	return json.Marshal(map[string]any{"language": "en", "title": "Water", "blocks": []artifact.Block{page}})
}

func waitState(t *testing.T, db *store.Store, id, state string) store.GenerationJob {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := db.GenerationJob(t.Context(), "teacher", id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == state {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	job, err := db.GenerationJob(t.Context(), "teacher", id)
	t.Fatalf("job never reached %s: %+v %v", state, job, err)
	return job
}

func TestDurableGenerationCancelRetryFailureAndRecovery(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "jobs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	provider := &mockProvider{started: make(chan struct{})}
	service := generate.New(db, provider, 20)
	manager := New(db, service)
	request, err := service.Create(ctx, "teacher", generate.Input{Type: artifact.SlidesType, Kind: "presentation", SlideCount: 1, Topic: "Water", Language: artifact.English})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Submit(ctx, "teacher", request.ID, 0); !errors.Is(err, store.ErrConflict) {
		t.Fatal("unconfirmed job queued", err)
	}
	preview, err := service.Preview(ctx, "teacher", request.ID, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Submit(ctx, "teacher", request.ID, 1); !errors.Is(err, store.ErrConflict) {
		t.Fatal("preview job queued", err)
	}
	if _, err := service.Confirm(ctx, "teacher", request.ID, preview.Version); err != nil {
		t.Fatal(err)
	}
	queued, err := manager.Submit(ctx, "teacher", request.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Submit(ctx, "teacher", request.ID, 1); !errors.Is(err, store.ErrConflict) {
		t.Fatal("duplicate queued", err)
	}
	if _, err := manager.Cancel(ctx, "teacher", queued.ID); err != nil {
		t.Fatal(err)
	}
	waitState(t, db, queued.ID, "cancelled")
	if provider.calls.Load() != 0 {
		t.Fatal("queued cancellation called model")
	}
	running, err := manager.Submit(ctx, "teacher", request.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	// A claimed job interrupted by process loss returns to the persisted queue.
	if _, err := db.ClaimNextGeneration(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.RecoverGenerationJobs(ctx); err != nil {
		t.Fatal(err)
	}
	waitState(t, db, running.ID, "queued")
	workCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { manager.Run(workCtx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-provider.started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	if _, err := manager.Cancel(ctx, "teacher", running.ID); err != nil {
		t.Fatal(err)
	}
	waitState(t, db, running.ID, "cancelled")
	current, err := db.Request(ctx, "teacher", request.ID)
	if err != nil || current.State != template.Confirmed {
		t.Fatal("cancel blocked retry", err)
	}
	retry, err := manager.Submit(ctx, "teacher", request.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	failed := waitState(t, db, retry.ID, "failed")
	if failed.ErrorCode != "subscription_limit" {
		t.Fatalf("lost actionable error %+v", failed)
	}
	retry, err = manager.Submit(ctx, "teacher", request.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	complete := waitState(t, db, retry.ID, "completed")
	if complete.Progress != 100 {
		t.Fatal("incorrect progress")
	}
	deck, err := db.Document(ctx, "teacher", request.ID, 0)
	if err != nil || deck.Type != artifact.SlidesType || deck.Revision != 1 {
		t.Fatal("worker did not persist deck", err)
	}
	if _, err := db.GenerationJob(ctx, "other", retry.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("cross-user job readable", err)
	}
}
