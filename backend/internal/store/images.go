package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"

	"eduforge/backend/internal/artifact"
)

var ErrImageLimit = errors.New("image upload limit reached")

// SaveSlideImage decodes bounded raster input and re-encodes it without metadata.
// Files remain immutable so older revisions continue to display their images.
func (s *Store) SaveSlideImage(ctx context.Context, user, deck string, data []byte) (_ string, err error) {
	if len(data) == 0 || len(data) > 8<<20 {
		return "", artifact.ErrInvalid
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 8_000_000 {
		return "", artifact.ErrInvalid
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", artifact.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.imageDir, 0700); err != nil {
		return "", fmt.Errorf("create image directory: %w", err)
	}
	id := artifact.NewID()
	filename := id + ".png"
	path := filepath.Join(s.imageDir, filename)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("create image file: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(path))
		}
	}()
	encodeErr := png.Encode(file, decoded)
	if err = errors.Join(encodeErr, file.Close()); err != nil {
		return "", fmt.Errorf("write image: %w", err)
	}
	err = s.transaction(ctx, func(tx *sql.Tx) error {
		kind, _, _, err := readHead(ctx, tx, user, deck)
		if err != nil {
			return err
		}
		if kind != artifact.SlidesType {
			return artifact.ErrInvalid
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM slide_images WHERE user_id = ? AND artifact_id = ?`, user, deck).Scan(&count); err != nil {
			return err
		}
		if count >= 200 {
			return ErrImageLimit
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO slide_images(user_id, artifact_id, id, filename) VALUES (?, ?, ?, ?)`, user, deck, id, filename)
		return err
	})
	if err != nil {
		return "", err
	}
	return "/api/v1/slides/" + deck + "/images/" + id, nil
}

func (s *Store) SlideImagePath(ctx context.Context, user, deck, id string) (string, error) {
	var filename string
	err := s.db.QueryRowContext(ctx, `SELECT filename FROM slide_images WHERE user_id = ? AND artifact_id = ? AND id = ?`, user, deck, id).Scan(&filename)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read image: %w", err)
	}
	return filepath.Join(s.imageDir, filename), nil
}

func validateImageReferences(ctx context.Context, tx *sql.Tx, user, deck string, content artifact.Content) error {
	for _, block := range artifact.FlattenBlocks(content.Blocks) {
		owner, id, local := artifact.LocalSlideImage(block.ImageURL)
		if !local {
			continue
		}
		if owner != deck {
			return artifact.ErrInvalid
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM slide_images WHERE user_id = ? AND artifact_id = ? AND id = ?)`, user, deck, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return artifact.ErrInvalid
		}
	}
	return nil
}
