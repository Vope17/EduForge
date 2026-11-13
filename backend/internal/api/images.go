package api

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"

	"eduforge/backend/internal/store"
)

func (d *Documents) uploadImage(w http.ResponseWriter, r *http.Request) {
	if !methodAllowed(w, r, http.MethodPost) {
		return
	}
	// A non-simple request header prevents cross-origin form uploads.
	if r.Header.Get("X-EduForge-Upload") != "1" {
		writeError(w, r, http.StatusBadRequest, "invalid_input", "Use the image upload control.")
		return
	}
	if _, err := d.db.Document(r.Context(), defaultUser, r.PathValue("id"), 0); err != nil {
		respond(w, r, 0, nil, err)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "body_too_large", "Image exceeds 8 MiB.")
		} else {
			writeError(w, r, http.StatusBadRequest, "invalid_input", "Cannot read image.")
		}
		return
	}
	url, err := d.db.SaveSlideImage(r.Context(), defaultUser, r.PathValue("id"), data)
	if errors.Is(err, store.ErrImageLimit) {
		writeError(w, r, http.StatusTooManyRequests, "image_limit", "Image limit reached for this presentation.")
		return
	}
	respond(w, r, http.StatusCreated, struct {
		URL string `json:"image_url"`
	}{url}, err)
}

func (d *Documents) image(w http.ResponseWriter, r *http.Request) {
	if !methodAllowed(w, r, http.MethodGet, http.MethodHead) {
		return
	}
	path, err := d.db.SlideImagePath(r.Context(), defaultUser, r.PathValue("id"), r.PathValue("image"))
	if err != nil {
		respond(w, r, 0, nil, err)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		respond(w, r, 0, nil, err)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.ErrorContext(r.Context(), "close image file", "error", err)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		respond(w, r, 0, nil, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, "image.png", info.ModTime(), file)
}
