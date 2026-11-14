package app

import (
	"context"
	"errors"

	"eduforge/internal/domain"
	styles "eduforge/internal/style"
)

func (a *App) validateStyle(ctx context.Context, owner string, style domain.Style) error {
	return styles.Validate(style)
}

func validateImages(a domain.Artifact, k domain.Knowledge) error {
	known := map[string]bool{}
	for _, img := range k.Images {
		known[img.Path] = true
	}
	for _, section := range a.Sections {
		paths := append([]string{section.Image}, section.Figures...)
		for _, el := range section.Layout {
			if el.Kind == "image" {
				paths = append(paths, el.Path)
			}
		}
		for _, path := range paths {
			if path != "" && !known[path] {
				return errors.New("image_not_in_artifact_sources")
			}
		}
	}
	return nil
}
