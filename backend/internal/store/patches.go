package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"eduforge/backend/internal/artifact"
)

func (s *Store) SaveProposal(ctx context.Context, user, id string, base int, anchors []artifact.Anchor, edits []artifact.Edit) (string, error) {
	proposalID := artifact.NewID()
	anchorJSON, err := json.Marshal(anchors)
	if err != nil {
		return "", fmt.Errorf("encode selection: %w", err)
	}
	editJSON, err := json.Marshal(edits)
	if err != nil {
		return "", fmt.Errorf("encode proposed edits: %w", err)
	}
	err = s.transaction(ctx, func(tx *sql.Tx) error {
		_, _, head, err := readHead(ctx, tx, user, id)
		if err != nil {
			return err
		}
		if head != base {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO patch_proposals(user_id,id,artifact_id,base_revision,anchors_json,edits_json)
			VALUES (?, ?, ?, ?, ?, ?)`, user, proposalID, id, base, string(anchorJSON), string(editJSON))
		return err
	})
	return proposalID, err
}

func (s *Store) AcceptProposal(ctx context.Context, user, id, proposalID string) (artifact.Artifact, error) {
	var result artifact.Artifact
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		kind, language, head, err := readHead(ctx, tx, user, id)
		if err != nil {
			return err
		}
		var base, accepted int
		var state string
		var anchorJSON, editJSON []byte
		err = tx.QueryRowContext(ctx, `SELECT base_revision,state,accepted_revision,anchors_json,edits_json
			FROM patch_proposals WHERE user_id = ? AND artifact_id = ? AND id = ?`,
			user, id, proposalID).Scan(&base, &state, &accepted, &anchorJSON, &editJSON)
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		current, err := readRevision(ctx, tx, user, id, head)
		if err != nil {
			return err
		}
		if state == "accepted" && head == accepted {
			result = documentFrom(id, kind, language, head, current)
			return nil
		}
		if state != "pending" || head != base {
			return ErrConflict
		}
		var anchors []artifact.Anchor
		var edits []artifact.Edit
		if err := json.Unmarshal(anchorJSON, &anchors); err != nil {
			return err
		}
		if err := json.Unmarshal(editJSON, &edits); err != nil {
			return err
		}
		content, err := artifact.ApplyEdits(id, kind, current.content, anchors, edits)
		if err != nil {
			return err
		}
		next := revision{content: content, origin: head + 1, undo: current.origin}
		if err := insertRevision(ctx, tx, user, id, head+1, next); err != nil {
			return err
		}
		if err := requireUpdate(tx.ExecContext(ctx, `UPDATE artifacts SET current_revision = ?
			WHERE user_id = ? AND id = ? AND current_revision = ?`, head+1, user, id, head)); err != nil {
			return err
		}
		if err := requireUpdate(tx.ExecContext(ctx, `UPDATE patch_proposals SET state = 'accepted', accepted_revision = ?
			WHERE user_id = ? AND artifact_id = ? AND id = ? AND state = 'pending'`, head+1, user, id, proposalID)); err != nil {
			return err
		}
		result = documentFrom(id, kind, language, head+1, next)
		return nil
	})
	return result, err
}

func (s *Store) RejectProposal(ctx context.Context, user, id, proposalID string) error {
	return requireUpdate(s.db.ExecContext(ctx, `UPDATE patch_proposals SET state = 'rejected'
		WHERE user_id = ? AND artifact_id = ? AND id = ? AND state = 'pending'`, user, id, proposalID))
}
