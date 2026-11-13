package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"eduforge/backend/internal/artifact"
	"eduforge/backend/internal/render"
)

// Limit simultaneous in-memory image/font packages for this single-user server.
var exportSlot = make(chan struct{}, 1)

func (d *Documents) download(w http.ResponseWriter, r *http.Request) {
	if !methodAllowed(w, r, http.MethodGet) {
		return
	}
	format := r.URL.Query().Get("format")
	if (d.kind == artifact.DocumentType && format != "docx" && format != "pdf") ||
		(d.kind == artifact.SlidesType && format != "pptx" && format != "pdf") {
		writeError(w, r, http.StatusBadRequest, "invalid_input", "Unsupported download format.")
		return
	}
	revision, err := strconv.Atoi(r.URL.Query().Get("revision"))
	if err != nil || revision < 1 {
		writeError(w, r, http.StatusBadRequest, "invalid_input", "Choose a saved revision.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	select {
	case exportSlot <- struct{}{}:
		defer func() { <-exportSlot }()
	case <-ctx.Done():
		writeError(w, r, http.StatusServiceUnavailable, "export_failed", "Export is busy. Try again.")
		return
	}
	document, err := d.db.Document(ctx, defaultUser, r.PathValue("id"), revision)
	if err != nil {
		respond(w, r, 0, nil, err)
		return
	}
	request, err := d.db.Request(ctx, defaultUser, document.ID)
	if err != nil {
		respond(w, r, 0, nil, err)
		return
	}
	if request.Spec == nil {
		writeError(w, r, http.StatusConflict, "version_conflict", "The confirmed template is missing.")
		return
	}
	input := render.Input{Artifact: document, Spec: *request.Spec, Images: map[string]render.Image{}}
	fontName := "NotoSansTC.ttf"
	if request.Spec.Font == "serif" {
		fontName = "NotoSerifTC.ttf"
	}
	fontDir := os.Getenv("EDUFORGE_FONT_DIR")
	if fontDir == "" {
		fontDir = "/app/fonts"
	}
	input.Font, err = os.ReadFile(filepath.Join(fontDir, fontName))
	if err == nil {
		err = d.exportImages(ctx, document, input.Images)
	}
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "export_assets_failed", "Cannot load export fonts or images. Check image sources and try again.")
		return
	}
	officeFormat := "docx"
	if d.kind == artifact.SlidesType {
		officeFormat = "pptx"
	}
	data, err := render.Office(ctx, input, officeFormat)
	if err == nil && format == "pdf" {
		data, err = render.PDF(ctx, data, officeFormat)
	}
	if errors.Is(err, render.ErrLayout) {
		writeError(w, r, http.StatusUnprocessableEntity, "export_layout_failed", "Reduce slide text or split it across more slides.")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "export_failed", "Cannot create download. Try again.")
		return
	}
	contentType := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	if format == "pptx" {
		contentType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	}
	if format == "pdf" {
		contentType = "application/pdf"
	}
	filename := document.Content.Title + "-r" + strconv.Itoa(document.Revision) + "." + format
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if _, err := w.Write(data); err != nil {
		slog.ErrorContext(ctx, "write artifact download", "error", err)
	}
}

func (d *Documents) exportImages(ctx context.Context, document artifact.Artifact, images map[string]render.Image) error {
	total := 0
	for _, block := range artifact.FlattenBlocks(document.Content.Blocks) {
		if block.Kind != "image" || block.ImageURL == "" {
			continue
		}
		if _, ok := images[block.ImageURL]; ok {
			continue
		}
		var data []byte
		var err error
		if owner, id, local := artifact.LocalSlideImage(block.ImageURL); local {
			if owner != document.ID {
				return fmt.Errorf("image owner mismatch")
			}
			var path string
			path, err = d.db.SlideImagePath(ctx, defaultUser, owner, id)
			if err == nil {
				data, err = os.ReadFile(path)
			}
		} else {
			data, err = render.FetchImage(ctx, block.ImageURL)
		}
		if err != nil {
			return fmt.Errorf("load export image: %w", err)
		}
		img, err := render.DecodeImage(data)
		if err != nil {
			return fmt.Errorf("decode export image: %w", err)
		}
		total += len(img.Data)
		if total > 64<<20 {
			return fmt.Errorf("export image limit exceeded")
		}
		images[block.ImageURL] = img
	}
	return nil
}
