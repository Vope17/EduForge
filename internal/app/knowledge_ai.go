package app

import (
	"context"
	"encoding/json"
	"errors"

	"eduforge/internal/domain"
	"eduforge/internal/providers"
	"eduforge/internal/research"
	"eduforge/internal/store"
)

// The shared knowledge is written by a model when one is connected: key
// points per section with their source passages, key terms and the
// evaluation conditions (see research.KnowledgePrompt). Without a model the
// rule-based source excerpts stay.

// uiLanguage is the user's interface language (zh-TW by default).
func (a *App) uiLanguage(ctx context.Context, owner string) string {
	items, err := a.Store.List(ctx, owner, store.Filter{Kind: "preference"})
	if err != nil {
		return "zh-TW"
	}
	for _, r := range items {
		var p struct {
			Key      string `json:"key"`
			Value    string `json:"value"`
			Imported bool   `json:"imported_candidate"`
		}
		_ = json.Unmarshal(r.Data, &p)
		if p.Key == "ui_language" && !p.Imported && domain.Language(p.Value) {
			return p.Value
		}
	}
	return "zh-TW"
}

// writeKnowledge asks a model to write k's claims, terms and conditions.
func (a *App) writeKnowledge(ctx context.Context, owner string, k domain.Knowledge) (domain.Knowledge, error) {
	if len(k.Units) == 0 {
		return k, errors.New("knowledge_required")
	}
	refs, err := a.ProviderRefs(ctx, owner)
	if err != nil {
		return k, err
	}
	lang := a.uiLanguage(ctx, owner)
	var out domain.Knowledge
	images := []string{}
	for _, u := range research.KnowledgePages(k) {
		if path, err := a.Store.Path(u.Preview); err == nil {
			images = append(images, path)
		}
	}
	_, _, err = a.Providers.Complete(providers.WithImages(ctx, images), owner, refs, research.KnowledgePrompt(k, lang), func(output string) error {
		parsed, err := research.ParseKnowledge(output, k)
		if err == nil {
			out = parsed
		}
		return err
	})
	if err != nil {
		return k, err
	}
	out.Language = lang
	return out, nil
}

// RewriteKnowledge has a model write a new version of a study's shared
// knowledge from its parsed sources. The new version needs reviewing again.
func (a *App) RewriteKnowledge(ctx context.Context, owner, paperID string, expected int) (domain.Record, error) {
	paper, p, err := get[domain.Paper](ctx, a, owner, paperID, "paper")
	if err != nil {
		return domain.Record{}, err
	}
	old, oldK, err := get[domain.Knowledge](ctx, a, owner, p.KnowledgeID, "knowledge")
	if err != nil {
		return domain.Record{}, err
	}
	if expected != old.Version {
		return domain.Record{}, domain.ErrConflict
	}
	k, err := a.writeKnowledge(ctx, owner, oldK)
	if err != nil {
		return domain.Record{}, err
	}
	k.Confirmed = false
	k.Edits = oldK.Edits + 1
	r, err := a.Store.Create(ctx, domain.Record{Owner: owner, Kind: "knowledge", Parent: paperID, Project: paper.Project, Paper: paperID, Version: old.Version + 1, Data: domain.Encode(k)})
	if err != nil {
		return r, err
	}
	_, err = set[domain.Paper](ctx, a, owner, paperID, paper.Version, func(p *domain.Paper) error {
		p.KnowledgeID = r.ID
		return nil
	})
	if err == nil {
		a.Store.Event(ctx, owner, paper.Project, paperID, "knowledge_written_by_ai", map[string]any{"previous": old.ID, "current": r.ID, "claims": len(k.Claims)})
	}
	return r, err
}
