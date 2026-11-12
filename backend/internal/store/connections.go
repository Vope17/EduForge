package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
)

func (s *Store) ModelRegistration(ctx context.Context, user string) (host, client, subject string, err error) {
	var data [16]byte
	rand.Read(data[:])
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	hostID := fmt.Sprintf("urn:uuid:%x-%x-%x-%x-%x", data[:4], data[4:6], data[6:8], data[8:10], data[10:])
	if _, err := s.db.ExecContext(ctx, `INSERT INTO model_registrations(user_id, host_id) VALUES (?, ?)
		ON CONFLICT(user_id) DO NOTHING`, user, hostID); err != nil {
		return "", "", "", fmt.Errorf("prepare model registration: %w", err)
	}
	err = s.db.QueryRowContext(ctx, `SELECT host_id, client_id, subject FROM model_registrations WHERE user_id = ?`, user).
		Scan(&host, &client, &subject)
	if err != nil {
		err = fmt.Errorf("read model registration: %w", err)
	}
	return
}

func (s *Store) SaveModelRegistration(ctx context.Context, user, client, subject string) error {
	return requireUpdate(s.db.ExecContext(ctx, `UPDATE model_registrations SET client_id = ?, subject = ? WHERE user_id = ?`,
		client, subject, user))
}

func (s *Store) ReserveGeneration(ctx context.Context, user string, limit int) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		return reserveGeneration(ctx, tx, user, limit)
	})
}
