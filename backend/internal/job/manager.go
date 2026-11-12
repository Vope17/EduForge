package job

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"eduforge/backend/internal/generate"
	"eduforge/backend/internal/llm"
	"eduforge/backend/internal/store"
)

type Manager struct {
	store      *store.Store
	generation *generate.Service
	wake       chan struct{}
	mu         sync.Mutex
	active     map[string]context.CancelFunc
}

func New(db *store.Store, generation *generate.Service) *Manager {
	return &Manager{store: db, generation: generation, wake: make(chan struct{}, 1), active: make(map[string]context.CancelFunc)}
}

func (m *Manager) Submit(ctx context.Context, user, id string, version int) (store.GenerationJob, error) {
	if !m.generation.Ready() {
		return store.GenerationJob{}, llm.ErrUnavailable
	}
	job, err := m.store.EnqueueGeneration(ctx, user, id, version)
	if err == nil {
		select {
		case m.wake <- struct{}{}:
		default:
		}
	}
	return job, err
}

func (m *Manager) Cancel(ctx context.Context, user, id string) (store.GenerationJob, error) {
	if err := m.store.CancelGeneration(ctx, user, id); err != nil {
		return store.GenerationJob{}, err
	}
	m.mu.Lock()
	if cancel := m.active[id]; cancel != nil {
		cancel()
	}
	m.mu.Unlock()
	return m.store.GenerationJob(ctx, user, id)
}

// Run processes the durable queue sequentially for this single-user server.
func (m *Manager) Run(ctx context.Context) {
	for ctx.Err() == nil {
		job, err := m.store.ClaimNextGeneration(ctx)
		if err == nil {
			m.execute(ctx, job)
			continue
		}
		if !errors.Is(err, store.ErrNotFound) && ctx.Err() == nil {
			slog.ErrorContext(ctx, "read generation queue")
		}
		select {
		case <-ctx.Done():
		case <-m.wake:
		case <-time.After(time.Second):
		}
	}
}

func (m *Manager) execute(ctx context.Context, job store.GenerationJob) {
	workCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.active[job.ID] = cancel
	m.mu.Unlock()
	current, err := m.store.GenerationJob(ctx, job.UserID, job.ID)
	if err != nil || current.CancelRequested {
		cancel()
	}
	_, generationErr := m.generation.Generate(workCtx, job.UserID, job.ArtifactID, job.TemplateVersion)
	state, code := "completed", ""
	switch {
	case generationErr == nil:
	case ctx.Err() != nil:
		state = "queued"
	case workCtx.Err() != nil:
		state = "cancelled"
	default:
		state, code = "failed", ErrorCode(generationErr)
	}
	cancel()
	m.mu.Lock()
	delete(m.active, job.ID)
	m.mu.Unlock()

	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	if err := m.store.FinishGenerationJob(finishCtx, job, state, code); err != nil {
		slog.ErrorContext(finishCtx, "finish generation job")
	}
}

func ErrorCode(err error) string {
	switch {
	case errors.Is(err, llm.ErrUnavailable):
		return "provider_unavailable"
	case errors.Is(err, llm.ErrPlanPermission):
		return "plan_permission"
	case errors.Is(err, llm.ErrSubscriptionLimit):
		return "subscription_limit"
	case errors.Is(err, store.ErrLimit):
		return "generation_limit"
	case errors.Is(err, store.ErrConflict):
		return "version_conflict"
	case errors.Is(err, llm.ErrInvalidOutput):
		return "invalid_generation"
	default:
		return "generation_failed"
	}
}
