package store

import (
	"context"
	"encoding/json"

	"eduforge/internal/domain"
)

// CommitTasks atomically locks a preparation request and enqueues its branches.
// A failed insert cannot leave a partially dispatched batch behind.
func (s *Store) CommitTasks(ctx context.Context, owner, id string, expected int, data json.RawMessage, tasks []domain.Record) ([]domain.Record, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	stamp := domain.Now()
	res, err := tx.ExecContext(ctx, "UPDATE records SET data=?,version=version+1,updated_at=? WHERE id=? AND owner_user_id=? AND kind='request' AND version=? AND json_extract(data,'$.status')='preparing'", string(data), stamp, id, owner, expected)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil, domain.ErrConflict
	}
	for i := range tasks {
		r := &tasks[i]
		r.ID = domain.ID()
		r.Owner = owner
		r.Kind = "task"
		r.Parent = id
		r.Version = 1
		r.Created = stamp
		r.Updated = stamp
		_, err = tx.ExecContext(ctx, "INSERT INTO records VALUES(?,?,?,?,?,?,?,?,?,?)", r.ID, r.Owner, r.Kind, r.Parent, nullable(r.Course), nullable(r.Lesson), r.Version, string(r.Data), r.Created, r.Updated)
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return tasks, nil
}
