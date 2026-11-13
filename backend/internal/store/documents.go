package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"eduforge/backend/internal/artifact"
)

type revision struct {
	content artifact.Content
	origin  int
	undo    int
	redo    []int
}

type RevisionSummary struct {
	Number    int
	Title     string
	CreatedAt string
}

type Change struct {
	BaseRevision   int
	Action         string
	Content        *artifact.Content
	TargetRevision int
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readRevision(ctx context.Context, db queryer, user, id string, number int) (revision, error) {
	var result revision
	var content, redo []byte
	err := db.QueryRowContext(ctx, `SELECT content_json, origin_revision, undo_revision, redo_json
		FROM artifact_revisions WHERE user_id = ? AND artifact_id = ? AND revision = ?`, user, id, number).
		Scan(&content, &result.origin, &result.undo, &redo)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, fmt.Errorf("read revision: %w", err)
	}
	if err := json.Unmarshal(content, &result.content); err != nil {
		return result, fmt.Errorf("decode revision: %w", err)
	}
	if err := json.Unmarshal(redo, &result.redo); err != nil {
		return result, fmt.Errorf("decode redo history: %w", err)
	}
	return result, nil
}

func insertRevision(ctx context.Context, tx *sql.Tx, user, id string, number int, value revision) error {
	if err := validateImageReferences(ctx, tx, user, id, value.content); err != nil {
		return err
	}
	content, err := json.Marshal(value.content)
	if err != nil {
		return fmt.Errorf("encode revision: %w", err)
	}
	redo, err := json.Marshal(value.redo)
	if err != nil {
		return fmt.Errorf("encode redo history: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO artifact_revisions
		(user_id, artifact_id, revision, content_json, origin_revision, undo_revision, redo_json) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		user, id, number, string(content), value.origin, value.undo, string(redo))
	if err != nil {
		return fmt.Errorf("insert revision: %w", err)
	}
	for _, block := range artifact.FlattenBlocks(value.content.Blocks) {
		_, err := tx.ExecContext(ctx, `INSERT INTO revision_blocks(user_id, artifact_id, revision, block_id) VALUES (?, ?, ?, ?)`,
			user, id, number, block.ID)
		if err != nil {
			return fmt.Errorf("index revision block: %w", err)
		}
	}
	return nil
}

func readHead(ctx context.Context, db queryer, user, id string) (artifact.Type, artifact.Language, int, error) {
	var kind artifact.Type
	var language artifact.Language
	var head int
	err := db.QueryRowContext(ctx, `SELECT r.artifact_type, r.language, a.current_revision FROM artifacts a
		JOIN document_requests r ON r.user_id = a.user_id AND r.id = a.id
		WHERE a.user_id = ? AND a.id = ?`, user, id).Scan(&kind, &language, &head)
	if errors.Is(err, sql.ErrNoRows) {
		return kind, language, head, ErrNotFound
	}
	if err != nil {
		return kind, language, head, fmt.Errorf("read document head: %w", err)
	}
	return kind, language, head, nil
}

func (s *Store) Document(ctx context.Context, user, id string, number int) (artifact.Artifact, error) {
	kind, language, head, err := readHead(ctx, s.db, user, id)
	if err != nil {
		return artifact.Artifact{}, err
	}
	if number == 0 {
		number = head
	}
	snapshot, err := readRevision(ctx, s.db, user, id, number)
	if err != nil {
		return artifact.Artifact{}, err
	}
	return documentFrom(id, kind, language, number, snapshot), nil
}

func documentFrom(id string, kind artifact.Type, language artifact.Language, number int, snapshot revision) artifact.Artifact {
	return artifact.Artifact{
		ID: id, Type: kind, Language: language, Revision: number, Content: snapshot.content,
		CanUndo: snapshot.undo > 0, CanRedo: len(snapshot.redo) > 0,
	}
}

func (s *Store) Revisions(ctx context.Context, user, id string) ([]RevisionSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT revision, json_extract(content_json, '$.title'), created_at
		FROM artifact_revisions WHERE user_id = ? AND artifact_id = ? ORDER BY revision DESC`, user, id)
	if err != nil {
		return nil, fmt.Errorf("list revisions: %w", err)
	}
	defer rows.Close()
	result := make([]RevisionSummary, 0)
	for rows.Next() {
		var item RevisionSummary
		if err := rows.Scan(&item.Number, &item.Title, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("read revision summary: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan revision summaries: %w", err)
	}
	if len(result) == 0 {
		return nil, ErrNotFound
	}
	return result, nil
}

func (s *Store) ChangeDocument(ctx context.Context, user, id string, change Change) (artifact.Artifact, error) {
	var document artifact.Artifact
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		kind, language, head, err := readHead(ctx, tx, user, id)
		if err != nil {
			return err
		}
		if head != change.BaseRevision {
			return ErrConflict
		}
		current, err := readRevision(ctx, tx, user, id, head)
		if err != nil {
			return err
		}
		next, err := changedRevision(ctx, tx, user, id, kind, head, current, change)
		if err != nil {
			return err
		}
		if err := insertRevision(ctx, tx, user, id, head+1, next); err != nil {
			return err
		}
		err = requireUpdate(tx.ExecContext(ctx, `UPDATE artifacts SET current_revision = ?
			WHERE user_id = ? AND id = ? AND current_revision = ?`, head+1, user, id, head))
		if err == nil {
			document = documentFrom(id, kind, language, head+1, next)
		}
		return err
	})
	return document, err
}

func changedRevision(ctx context.Context, tx *sql.Tx, user, id string, kind artifact.Type, head int, current revision, change Change) (revision, error) {
	switch change.Action {
	case "save":
		if change.Content == nil {
			return revision{}, artifact.ErrInvalid
		}
		content := *change.Content
		if err := content.ValidateFor(kind); err != nil {
			return revision{}, err
		}
		if err := content.AssignIDs(current.content.Blocks); err != nil {
			return revision{}, err
		}
		return revision{content: content, origin: head + 1, undo: current.origin}, nil
	case "restore":
		target, err := readRevision(ctx, tx, user, id, change.TargetRevision)
		return revision{content: target.content, origin: head + 1, undo: current.origin}, err
	case "undo":
		if current.undo == 0 {
			return revision{}, ErrConflict
		}
		target, err := readRevision(ctx, tx, user, id, current.undo)
		target.redo = append([]int{current.origin}, current.redo...)
		return target, err
	case "redo":
		if len(current.redo) == 0 {
			return revision{}, ErrConflict
		}
		target, err := readRevision(ctx, tx, user, id, current.redo[0])
		target.redo = current.redo[1:]
		return target, err
	default:
		return revision{}, artifact.ErrInvalid
	}
}
