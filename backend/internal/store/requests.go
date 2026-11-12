package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"eduforge/backend/internal/artifact"
	"eduforge/backend/internal/template"
)

func (s *Store) CreateRequest(ctx context.Context, user string, request template.Request) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO document_requests (user_id, id, state, language, artifact_type, slide_count, topic, audience, kind)
		VALUES (?, ?, 'requested', ?, ?, ?, ?, ?, ?)`, user, request.ID, request.Language, request.Type, request.SlideCount, request.Topic, request.Audience, request.Kind)
	if err != nil {
		return fmt.Errorf("create document request: %w", err)
	}
	return nil
}

func (s *Store) Request(ctx context.Context, user, id string) (template.Request, error) {
	row := s.db.QueryRowContext(ctx, `SELECT r.id, r.state, r.language, r.artifact_type, r.slide_count, r.topic, r.audience, r.kind,
		r.template_version, r.confirmed_version, t.spec_json
		FROM document_requests r LEFT JOIN template_versions t
			ON t.user_id = r.user_id AND t.artifact_id = r.id AND t.version = r.template_version
		WHERE r.user_id = ? AND r.id = ?`, user, id)
	return readRequest(row)
}

func readRequest(row interface{ Scan(...any) error }) (template.Request, error) {
	var r template.Request
	var spec sql.NullString
	err := row.Scan(&r.ID, &r.State, &r.Language, &r.Type, &r.SlideCount, &r.Topic, &r.Audience, &r.Kind, &r.Version, &r.ConfirmedVersion, &spec)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, fmt.Errorf("read document request: %w", err)
	}
	if spec.Valid {
		r.Spec = new(template.Spec)
		if err := json.Unmarshal([]byte(spec.String), r.Spec); err != nil {
			return r, fmt.Errorf("decode stored template: %w", err)
		}
	}
	return r, nil
}

type RequestSummary struct {
	ID        string
	Title     string
	State     template.State
	CreatedAt string
}

func (s *Store) ListRequests(ctx context.Context, user string, kind artifact.Type) ([]RequestSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, COALESCE(json_extract(v.content_json, '$.title'), r.topic),
		r.state, r.created_at FROM document_requests r
		LEFT JOIN artifacts a ON a.user_id = r.user_id AND a.id = r.id
		LEFT JOIN artifact_revisions v ON v.user_id = a.user_id AND v.artifact_id = a.id AND v.revision = a.current_revision
		WHERE r.user_id = ? AND r.artifact_type = ? ORDER BY r.created_at DESC, r.id DESC`, user, kind)
	if err != nil {
		return nil, fmt.Errorf("list document requests: %w", err)
	}
	defer rows.Close()
	result := make([]RequestSummary, 0)
	for rows.Next() {
		var item RequestSummary
		if err := rows.Scan(&item.ID, &item.Title, &item.State, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("read document summary: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ClaimPreview(ctx context.Context, user string, r template.Request, limit int) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		err := requireUpdate(tx.ExecContext(ctx, `UPDATE document_requests SET preview_busy = 1
			WHERE user_id = ? AND id = ? AND state IN ('requested','awaiting_confirmation')
			AND template_version = ? AND preview_busy = 0`, user, r.ID, r.Version))
		if err != nil {
			return err
		}
		return reserveGeneration(ctx, tx, user, limit)
	})
}

func (s *Store) ReleasePreview(ctx context.Context, user, id string, version int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE document_requests SET preview_busy = 0
		WHERE user_id = ? AND id = ? AND template_version = ?`, user, id, version)
	return err
}

func (s *Store) PublishPreview(ctx context.Context, user string, r template.Request, spec template.Spec) error {
	data, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("encode template: %w", err)
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		err := requireUpdate(tx.ExecContext(ctx, `UPDATE document_requests SET state = 'template_generated'
			WHERE user_id = ? AND id = ? AND template_version = ? AND preview_busy = 1
			AND state IN ('requested','awaiting_confirmation')`, user, r.ID, r.Version))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO template_versions(user_id, artifact_id, version, spec_json)
			VALUES (?, ?, ?, ?)`, user, r.ID, r.Version+1, string(data)); err != nil {
			return err
		}
		return requireUpdate(tx.ExecContext(ctx, `UPDATE document_requests
			SET state = 'awaiting_confirmation', template_version = ?, preview_busy = 0
			WHERE user_id = ? AND id = ? AND state = 'template_generated'`, r.Version+1, user, r.ID))
	})
}

func (s *Store) Confirm(ctx context.Context, user, id string, version int) error {
	return requireUpdate(s.db.ExecContext(ctx, `UPDATE document_requests SET state = 'confirmed', confirmed_version = ?
		WHERE user_id = ? AND id = ? AND state = 'awaiting_confirmation' AND template_version = ?
		AND template_version > 0 AND preview_busy = 0`, version, user, id, version))
}

func (s *Store) ClaimGeneration(ctx context.Context, user string, r template.Request, limit int) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		err := requireUpdate(tx.ExecContext(ctx, `UPDATE document_requests SET state = 'generating'
			WHERE user_id = ? AND id = ? AND state = 'confirmed' AND confirmed_version = ?
			AND confirmed_version = template_version AND confirmed_version > 0`, user, r.ID, r.ConfirmedVersion))
		if err != nil {
			return err
		}
		return reserveGeneration(ctx, tx, user, limit)
	})
}

func (s *Store) ReleaseGeneration(ctx context.Context, user, id string, version int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE document_requests SET state = 'confirmed'
		WHERE user_id = ? AND id = ? AND state = 'generating' AND confirmed_version = ?`, user, id, version)
	return err
}

func (s *Store) CompleteGeneration(ctx context.Context, user string, r template.Request, content artifact.Content) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		err := requireUpdate(tx.ExecContext(ctx, `UPDATE document_requests SET state = 'ready'
			WHERE user_id = ? AND id = ? AND state = 'generating' AND confirmed_version = ?`, user, r.ID, r.ConfirmedVersion))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO artifacts(user_id, id, current_revision) VALUES (?, ?, 1)`, user, r.ID); err != nil {
			return err
		}
		return insertRevision(ctx, tx, user, r.ID, 1, revision{content: content, origin: 1})
	})
}
