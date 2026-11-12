package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"eduforge/backend/internal/artifact"
)

type GenerationJob struct {
	UserID          string
	ID              string
	ArtifactID      string
	TemplateVersion int
	State           string
	Progress        int
	ErrorCode       string
	CancelRequested bool
}

func (s *Store) EnqueueGeneration(ctx context.Context, user, id string, version int) (GenerationJob, error) {
	result := GenerationJob{UserID: user, ID: artifact.NewID(), ArtifactID: id, TemplateVersion: version, State: "queued"}
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var allowed bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM document_requests
			WHERE user_id = ? AND id = ? AND state = 'confirmed' AND template_version = ?
			AND confirmed_version = template_version AND confirmed_version > 0
			AND NOT EXISTS(SELECT 1 FROM generation_jobs WHERE user_id = ? AND artifact_id = ? AND state IN ('queued','running')))`,
			user, id, version, user, id).Scan(&allowed)
		if err != nil {
			return err
		}
		if !allowed {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO generation_jobs(user_id,id,artifact_id,template_version,state)
			VALUES (?, ?, ?, ?, 'queued')`, user, result.ID, id, version)
		return err
	})
	return result, err
}

type rowScanner interface{ Scan(...any) error }

func readJob(row rowScanner) (GenerationJob, error) {
	var result GenerationJob
	err := row.Scan(&result.UserID, &result.ID, &result.ArtifactID, &result.TemplateVersion,
		&result.State, &result.Progress, &result.ErrorCode, &result.CancelRequested)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, fmt.Errorf("read generation job: %w", err)
	}
	return result, nil
}

const jobColumns = "user_id,id,artifact_id,template_version,state,progress,error_code,cancel_requested"

func (s *Store) GenerationJob(ctx context.Context, user, id string) (GenerationJob, error) {
	return readJob(s.db.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM generation_jobs WHERE user_id = ? AND id = ?", user, id))
}

func (s *Store) LatestGeneration(ctx context.Context, user, id string) (GenerationJob, error) {
	return readJob(s.db.QueryRowContext(ctx, "SELECT "+jobColumns+
		" FROM generation_jobs WHERE user_id = ? AND artifact_id = ? ORDER BY sequence DESC LIMIT 1", user, id))
}

func (s *Store) ClaimNextGeneration(ctx context.Context) (GenerationJob, error) {
	var result GenerationJob
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = readJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+
			" FROM generation_jobs WHERE state = 'queued' AND cancel_requested = 0 ORDER BY sequence LIMIT 1"))
		if err != nil {
			return err
		}
		err = requireUpdate(tx.ExecContext(ctx, `UPDATE generation_jobs SET state = 'running', progress = 25
			WHERE user_id = ? AND id = ? AND state = 'queued'`, result.UserID, result.ID))
		result.State, result.Progress = "running", 25
		return err
	})
	return result, err
}

func (s *Store) FinishGenerationJob(ctx context.Context, job GenerationJob, state, code string) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		progress := 0
		if state == "completed" {
			progress = 100
		}
		if state != "completed" {
			_, err := tx.ExecContext(ctx, `UPDATE document_requests SET state = 'confirmed'
				WHERE user_id = ? AND id = ? AND state = 'generating' AND confirmed_version = ?`,
				job.UserID, job.ArtifactID, job.TemplateVersion)
			if err != nil {
				return err
			}
		}
		return requireUpdate(tx.ExecContext(ctx, `UPDATE generation_jobs SET state = ?, progress = ?, error_code = ?
			WHERE user_id = ? AND id = ? AND state = 'running'`, state, progress, code, job.UserID, job.ID))
	})
}

func (s *Store) CancelGeneration(ctx context.Context, user, id string) error {
	return requireUpdate(s.db.ExecContext(ctx, `UPDATE generation_jobs SET cancel_requested = 1,
		state = CASE WHEN state = 'queued' THEN 'cancelled' ELSE state END
		WHERE user_id = ? AND id = ? AND state IN ('queued','running')`, user, id))
}

func (s *Store) RecoverGenerationJobs(ctx context.Context) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE generation_jobs SET state = 'completed', progress = 100
			WHERE state IN ('queued','running') AND EXISTS (SELECT 1 FROM document_requests r
			WHERE r.user_id = generation_jobs.user_id AND r.id = generation_jobs.artifact_id AND r.state = 'ready')`)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE generation_jobs SET state = 'cancelled', progress = 0
			WHERE state IN ('queued','running') AND cancel_requested = 1`)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE generation_jobs SET state = 'queued', progress = 0
			WHERE state = 'running'`)
		return err
	})
}
