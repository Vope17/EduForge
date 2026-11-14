package app

import (
	"context"
	"errors"
	"path/filepath"

	"eduforge/internal/artifacts"
	"eduforge/internal/domain"
)

const templateHistoryLimit = 20

// TemplateSpec reads the editable design of an imported template.
func (a *App) TemplateSpec(ctx context.Context, owner, id string) (artifacts.TemplateSpec, error) {
	_, t, err := get[domain.Template](ctx, a, owner, id, "template")
	if err != nil {
		return artifacts.TemplateSpec{}, err
	}
	if t.File == "" {
		return artifacts.TemplateSpec{}, errors.New("template_not_editable")
	}
	file, err := a.Store.Path(t.File)
	if err != nil {
		return artifacts.TemplateSpec{}, err
	}
	return artifacts.ReadTemplateSpec(file, t)
}

// withHistory returns t's history with t itself (as record version v) added.
func withHistory(t domain.Template, version int, savedAt string) []domain.TemplateVersion {
	history := t.History
	t.History = nil
	entry := domain.TemplateVersion{Version: version, SavedAt: savedAt, Template: domain.Encode(t)}
	out := append([]domain.TemplateVersion{entry}, history...)
	if len(out) > templateHistoryLimit {
		out = out[:templateHistoryLimit]
	}
	return out
}

// SaveTemplateSpec writes an edited design into a new copy of the template
// file, re-reads it and keeps the previous state in the history. Outputs made
// earlier keep their own template snapshot.
func (a *App) SaveTemplateSpec(ctx context.Context, owner, id string, version int, spec artifacts.TemplateSpec) (domain.Record, error) {
	r, t, err := get[domain.Template](ctx, a, owner, id, "template")
	if err != nil {
		return r, err
	}
	if r.Version != version {
		return r, domain.ErrConflict
	}
	if t.File == "" {
		return r, errors.New("template_not_editable")
	}
	file, err := a.Store.Path(t.File)
	if err != nil {
		return r, err
	}
	b, err := artifacts.WriteTemplateSpec(file, t, spec)
	if err != nil {
		return r, err
	}
	rel, _, err := a.Store.Blob(b, filepath.Ext(t.File))
	if err != nil {
		return r, err
	}
	path, err := a.Store.Path(rel)
	if err != nil {
		return r, err
	}
	var next domain.Template
	if t.Kind == "docx" {
		next, err = artifacts.InspectWordTemplate(ctx, a.Store, path)
	} else {
		next, err = artifacts.InspectTemplateLayouts(ctx, a.Store, path, t.CoverLayout, t.ContentLayout)
		next.Kind = "pptx"
		// The content area is EduForge's own; the file does not record it.
		if spec.BodyBox != nil {
			next.BodyBox = spec.BodyBox
		}
	}
	if err != nil {
		return r, err
	}
	next.File, next.Name, next.Description, next.LayoutTypes = rel, t.Name, t.Description, t.LayoutTypes
	next.History = withHistory(t, r.Version, domain.Now())
	out, err := a.Store.Update(ctx, owner, id, version, func(row *domain.Record) error {
		row.Data = domain.Encode(next)
		return nil
	})
	if err == nil {
		a.Store.Event(ctx, owner, "", "", "template_edit", map[string]any{"template": id, "version": out.Version})
	}
	return out, err
}

// RestoreTemplate makes an earlier version current again (as a new version,
// so the restore itself can be undone).
func (a *App) RestoreTemplate(ctx context.Context, owner, id string, version, from int) (domain.Record, error) {
	r, t, err := get[domain.Template](ctx, a, owner, id, "template")
	if err != nil {
		return r, err
	}
	if r.Version != version {
		return r, domain.ErrConflict
	}
	var entry *domain.TemplateVersion
	for i := range t.History {
		if t.History[i].Version == from {
			entry = &t.History[i]
		}
	}
	if entry == nil {
		return r, domain.ErrNotFound
	}
	old, err := domain.Decode[domain.Template](entry.Template)
	if err != nil {
		return r, err
	}
	old.Name = t.Name
	old.History = withHistory(t, r.Version, domain.Now())
	return a.Store.Update(ctx, owner, id, version, func(row *domain.Record) error {
		row.Data = domain.Encode(old)
		return nil
	})
}
