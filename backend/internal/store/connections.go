package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ChatGPTModel returns the model chosen for the ChatGPT connection ("" = Codex default).
func (s *Store) ChatGPTModel(ctx context.Context, user string) (string, error) {
	var model string
	err := s.db.QueryRowContext(ctx, `SELECT chatgpt_model FROM model_settings WHERE user_id = ?`, user).Scan(&model)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read model settings: %w", err)
	}
	return model, nil
}

func (s *Store) SaveChatGPTModel(ctx context.Context, user, model string) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO model_settings(user_id, chatgpt_model) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET chatgpt_model = excluded.chatgpt_model`, user, model); err != nil {
		return fmt.Errorf("save model settings: %w", err)
	}
	return nil
}

func (s *Store) ReserveGeneration(ctx context.Context, user string, limit int) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		return reserveGeneration(ctx, tx, user, limit)
	})
}
