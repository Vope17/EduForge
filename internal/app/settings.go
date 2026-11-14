package app

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"eduforge/internal/artifacts"
	"eduforge/internal/domain"
	"eduforge/internal/research"
	"eduforge/internal/store"
)

func (a *App) Preference(ctx context.Context, owner, key string, value any) (domain.Record, error) {
	if key != "ui_language" {
		return domain.Record{}, errors.New("unsupported_preference")
	}
	lang, ok := value.(string)
	if !ok || !domain.Language(lang) {
		return domain.Record{}, errors.New("invalid_language")
	}
	items, err := a.Store.List(ctx, owner, store.Filter{Kind: "preference"})
	if err != nil {
		return domain.Record{}, err
	}
	data := map[string]any{"key": key, "value": value}
	for _, r := range items {
		var item map[string]any
		_ = json.Unmarshal(r.Data, &item)
		if item["key"] == key && item["imported_candidate"] != true {
			return a.Store.Update(ctx, owner, r.ID, r.Version, func(r *domain.Record) error {
				r.Data = domain.Encode(data)
				return nil
			})
		}
	}
	return a.create(ctx, owner, "preference", "", "", "", data)
}
func (a *App) ProviderRefs(ctx context.Context, owner string) ([]domain.ProviderRef, error) {
	records, err := a.Store.List(ctx, owner, store.Filter{Kind: "provider"})
	if err != nil {
		return nil, err
	}
	prefs, err := a.Store.List(ctx, owner, store.Filter{Kind: "preference"})
	if err != nil {
		return nil, err
	}
	order := []string{}
	for _, item := range prefs {
		var p struct {
			Key      string   `json:"key"`
			IDs      []string `json:"ids"`
			Imported bool     `json:"imported_candidate"`
		}
		_ = json.Unmarshal(item.Data, &p)
		if p.Key == "provider_order" && !p.Imported {
			order = p.IDs
		}
	}
	byID := map[string]domain.Record{}
	for _, r := range records {
		byID[r.ID] = r
	}
	out := []domain.ProviderRef{}
	seen := map[string]bool{}
	for _, id := range order {
		if r, ok := byID[id]; ok {
			p, _ := domain.Decode[domain.Provider](r.Data)
			out = append(out, domain.ProviderRef{ID: id, Provider: p})
			seen[id] = true
		}
	}
	for _, r := range records {
		if !seen[r.ID] {
			p, _ := domain.Decode[domain.Provider](r.Data)
			out = append(out, domain.ProviderRef{ID: r.ID, Provider: p})
		}
	}
	// The offline test mode never calls a model, so it is only a last resort
	// after every model provider, whatever the saved order says.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Provider.Kind != "source-extract" && out[j].Provider.Kind == "source-extract"
	})
	return out, nil
}
func (a *App) ManualReading(ctx context.Context, owner string, h domain.Reading) (domain.Record, error) {
	if strings.TrimSpace(h.Title) == "" {
		return domain.Record{}, errors.New("paper_title_required")
	}
	if h.PaperID != "" {
		return a.MarkRead(ctx, owner, h.PaperID, true)
	}
	h.CompletedAt = domain.Now()
	return a.create(ctx, owner, "reading", "", "", "", h)
}
func (a *App) ReapplyTemplate(ctx context.Context, owner, id, templateID string) (domain.Record, error) {
	old, art, err := get[domain.Artifact](ctx, a, owner, id, "artifact")
	if err != nil {
		return old, err
	}
	if art.Type != "A" && art.Type != "B" && art.Type != "C" {
		return old, errors.New("template_not_supported")
	}
	if art.Style.Resolved == nil {
		art.Style.Resolved = map[string]domain.Style{}
	}
	if art.Type != "B" {
		// A reader or plan keeps its style and takes (or drops) the Word template.
		style := maps.Clone(art.Style.Resolved[art.Type])
		if style == nil {
			style = domain.Style{}
		}
		for _, key := range artifacts.WordStyleKeys {
			delete(style, key)
		}
		if templateID != "" {
			t, err := a.templateOf(ctx, owner, templateID, "docx")
			if err != nil {
				return old, err
			}
			maps.Copy(style, artifacts.TemplateStyle(t))
		}
		art.Style.Resolved[art.Type] = style
		art.Settings.ReaderTemplateID = templateID
	} else {
		style := artifacts.DefaultStyle()
		if templateID != "" {
			t, err := a.templateOf(ctx, owner, templateID, "pptx")
			if err != nil {
				return old, err
			}
			style = artifacts.TemplateStyle(t)
		}
		// Boxes keep their place relative to the old and new title/content areas.
		artifacts.RemapDeck(art.Sections, artifacts.GeometryOf(art.Style.Resolved["B"]), artifacts.GeometryOf(style))
		art.Style.Resolved["B"] = style
		art.Settings.TemplateID = templateID
	}
	art.Previous = id
	art.CreatedFrom = "template_reapply"
	art.Edits++
	k, err := a.ArtifactKnowledge(ctx, owner, art)
	if err != nil {
		return old, err
	}
	art.Check = research.Validate(art, k)
	return a.Store.Create(ctx, domain.Record{Owner: owner, Kind: "artifact", Parent: old.Parent, Project: old.Project, Paper: old.Paper, Version: old.Version + 1, Data: domain.Encode(art)})
}
func (a *App) SaveSSH(ctx context.Context, owner, config, known, key string) error {
	if err := store.ValidateID(owner); err != nil {
		return err
	}
	if len(config) > 64<<10 || len(known) > 1<<20 || len(key) > 64<<10 {
		return errors.New("connection_config_too_large")
	}
	dir := filepath.Join(a.Secrets, "ssh", owner)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if key != "" {
		if err := store.Atomic(filepath.Join(dir, "identity"), []byte(key), 0600); err != nil {
			return err
		}
	}
	if config != "" {
		lower := strings.ToLower(config)
		for _, directive := range []string{"proxycommand", "localcommand", "permitlocalcommand", "match exec", "include ", "identityfile"} {
			if strings.Contains(lower, directive) {
				return errors.New("ssh_config_use_managed_identity_and_no_local_commands")
			}
		}
		config = "Host *\n  IdentitiesOnly yes\n  IdentityFile " + filepath.Join(dir, "identity") + "\n\n" + config
		if err := store.Atomic(filepath.Join(dir, "config"), []byte(config), 0600); err != nil {
			return err
		}
	}
	if known != "" {
		if err := store.Atomic(filepath.Join(dir, "known_hosts"), []byte(known), 0600); err != nil {
			return err
		}
	}
	return nil
}
