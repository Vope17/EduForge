// Package app orchestrates domain operations; every entrypoint takes an owner context.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"eduforge/internal/artifacts"
	"eduforge/internal/domain"
	"eduforge/internal/providers"
	"eduforge/internal/research"
	"eduforge/internal/store"
)

type App struct {
	Store      *store.Store
	Providers  *providers.Manager
	Secrets    string
	mu         sync.Mutex
	running    map[string]bool
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	feedbackMu sync.Mutex
}

func New(s *store.Store, secrets string) (*App, error) {
	abs, err := filepath.Abs(secrets)
	if err != nil {
		return nil, err
	}
	if storePathOverlap(s.Root, abs) {
		return nil, errors.New("secrets_must_be_outside_project_data")
	}
	if err = os.MkdirAll(abs, 0700); err != nil {
		return nil, err
	}
	_ = os.Chmod(abs, 0700)
	a := &App{Store: s, Providers: providers.New(s, abs), Secrets: abs, running: map[string]bool{}}
	return a, a.Bootstrap(context.Background(), s.LocalUser)
}
func storePathOverlap(a, b string) bool {
	rel, err := filepath.Rel(a, b)
	return err == nil && (rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."))
}
func (a *App) create(ctx context.Context, owner, kind, parent, project, paper string, data any) (domain.Record, error) {
	return a.Store.Create(ctx, domain.Record{Owner: owner, Kind: kind, Parent: parent, Project: project, Paper: paper, Data: domain.Encode(data)})
}
func set[T any](ctx context.Context, a *App, owner, id string, version int, fn func(*T) error) (domain.Record, error) {
	return a.Store.Update(ctx, owner, id, version, func(r *domain.Record) error {
		v, err := domain.Decode[T](r.Data)
		if err != nil {
			return err
		}
		if err = fn(&v); err != nil {
			return err
		}
		r.Data = domain.Encode(v)
		return nil
	})
}
func get[T any](ctx context.Context, a *App, owner, id, kind string) (domain.Record, T, error) {
	r, err := a.Store.Get(ctx, owner, id, kind)
	if err != nil {
		var zero T
		return r, zero, err
	}
	v, err := domain.Decode[T](r.Data)
	return r, v, err
}

func (a *App) Bootstrap(ctx context.Context, owner string) error {
	list, err := a.Store.List(ctx, owner, store.Filter{Kind: "provider"})
	if err != nil {
		return err
	}
	if len(list) == 0 {
		for _, p := range []domain.Provider{{Name: "Codex · ChatGPT", Kind: "codex", Enabled: true, MaxOutput: 2400}, {Name: "Claude Code", Kind: "claude-cli", MaxOutput: 2400}, {Name: "OpenAI API", Kind: "openai", Paid: true, MaxOutput: 2400}, {Name: "Anthropic API", Kind: "anthropic", Paid: true, MaxOutput: 2400}, {Name: store.OfflineProviderName, Kind: "source-extract", MaxOutput: 2400}} {
			if _, err = a.create(ctx, owner, "provider", "", "", "", p); err != nil {
				return err
			}
		}
	}
	return nil
}
func (a *App) CreateProject(ctx context.Context, owner string, p domain.Project) (domain.Record, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len([]rune(p.Name)) > 150 {
		return domain.Record{}, errors.New("project_name_required")
	}
	if p.Style == nil {
		p.Style = map[string]domain.Style{}
	}
	for _, s := range p.Style {
		if err := a.validateStyle(ctx, owner, s); err != nil {
			return domain.Record{}, err
		}
	}
	return a.create(ctx, owner, "project", "", "", "", p)
}
func NormalizeSettings(s domain.OutputSettings) domain.OutputSettings {
	if s.Depth == "" {
		s.Depth = "standard"
	}
	if s.Level == "" {
		s.Level = "intermediate"
	}
	if s.Audience == "" {
		s.Audience = "classmates"
	}
	if s.Minutes == 0 {
		s.Minutes = 10
	}
	if s.Languages == nil {
		s.Languages = map[string]string{}
	}
	return s
}
func validKinds(kinds []string) error {
	seen := map[string]bool{}
	if len(kinds) == 0 {
		return errors.New("select_output")
	}
	for _, kind := range kinds {
		if kind != "A" && kind != "B" {
			return errors.New("invalid_output")
		}
		if seen[kind] {
			return errors.New("duplicate_output")
		}
		seen[kind] = true
	}
	return nil
}
func validateOutputSettings(s domain.OutputSettings) error {
	if len(s.Selected) != 0 {
		if err := validKinds(s.Selected); err != nil {
			return err
		}
	}
	for kind, lang := range s.Languages {
		if !slices.Contains([]string{"A", "B"}, kind) {
			return errors.New("invalid_output")
		}
		if lang != "" && !domain.Language(lang) {
			return errors.New("invalid_language")
		}
	}
	if !slices.Contains([]string{"brief", "standard", "deep"}, s.Depth) ||
		!slices.Contains([]string{"beginner", "intermediate", "advanced"}, s.Level) ||
		s.Minutes < 1 || s.Minutes > 180 {
		return errors.New("invalid_output_settings")
	}
	return nil
}
func (a *App) CreatePaper(ctx context.Context, owner, project string, p domain.Paper) (domain.Record, error) {
	if _, err := a.Store.Get(ctx, owner, project, "project"); err != nil {
		return domain.Record{}, err
	}
	p.Title = strings.TrimSpace(p.Title)
	if p.Title == "" {
		return domain.Record{}, errors.New("paper_title_required")
	}
	p.KnowledgeID = ""
	p.Settings = NormalizeSettings(p.Settings)
	if err := validateOutputSettings(p.Settings); err != nil {
		return domain.Record{}, err
	}
	return a.create(ctx, owner, "paper", project, project, "", p)
}
func (a *App) SavePaper(ctx context.Context, owner, id string, version int, p domain.Paper) (domain.Record, error) {
	p.Settings = NormalizeSettings(p.Settings)
	if err := validateOutputSettings(p.Settings); err != nil {
		return domain.Record{}, err
	}
	return set[domain.Paper](ctx, a, owner, id, version, func(old *domain.Paper) error {
		if strings.TrimSpace(p.Title) == "" {
			return errors.New("paper_title_required")
		}
		old.Title = p.Title
		old.Group = p.Group
		old.Repo = p.Repo
		old.Settings = p.Settings
		return nil
	})
}
func (a *App) Upload(ctx context.Context, owner, paperID, name, role string, b []byte) (domain.Record, error) {
	paper, _, err := get[domain.Paper](ctx, a, owner, paperID, "paper")
	if err != nil {
		return domain.Record{}, err
	}
	ext := strings.ToLower(filepath.Ext(name))
	// Only research sources are accepted; presentation design references
	// were removed (looks come from the selected Office template).
	if role == "" {
		role = "research"
	}
	if role != "research" {
		return domain.Record{}, errors.New("invalid_source_role")
	}
	allowed := ext == ".pdf" || ext == ".pptx"
	if !allowed || len(b) == 0 || len(b) > 64<<20 {
		return domain.Record{}, errors.New("unsupported_file_or_size")
	}
	if ext == ".pdf" && !strings.HasPrefix(string(b[:min(8, len(b))]), "%PDF-") {
		return domain.Record{}, errors.New("invalid_pdf")
	}
	if ext == ".pptx" && (len(b) < 4 || string(b[:2]) != "PK") {
		return domain.Record{}, errors.New("invalid_office_file")
	}
	rel, sum, err := a.Store.Blob(b, ext)
	if err != nil {
		return domain.Record{}, err
	}
	r, err := a.create(ctx, owner, "source", paperID, paper.Project, paperID, domain.Source{Name: filepath.Base(name), Role: role, Path: rel, SHA256: sum, Size: int64(len(b))})
	if err == nil {
		a.Store.Event(ctx, owner, paper.Project, paperID, "source_uploaded", map[string]any{"id": r.ID, "role": role, "sha256": sum})
	}
	return r, err
}
func (a *App) QueueParse(ctx context.Context, owner, paperID string) (domain.Record, error) {
	r, _, err := get[domain.Paper](ctx, a, owner, paperID, "paper")
	if err != nil {
		return domain.Record{}, err
	}
	sources, err := a.Store.List(ctx, owner, store.Filter{Kind: "source", Paper: paperID})
	if err != nil {
		return domain.Record{}, err
	}
	count := 0
	for _, src := range sources {
		s, _ := domain.Decode[domain.Source](src.Data)
		if s.Role == "research" {
			count++
		}
	}
	if count == 0 {
		return domain.Record{}, errors.New("research_source_required")
	}
	return a.create(ctx, owner, "task", paperID, r.Project, paperID, domain.Task{Type: "parse", Status: "queued", Stage: "parse", Total: count})
}
func (a *App) SaveKnowledge(ctx context.Context, owner, paperID string, expected int, k domain.Knowledge) (domain.Record, error) {
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
	allowed := map[string]bool{}
	for _, u := range oldK.Units {
		allowed[u.ID] = true
	}
	for _, claim := range k.Claims {
		if claim.Section < 0 || claim.Section >= len(research.Titles["en"]) {
			return domain.Record{}, errors.New("invalid_section")
		}
		for _, cite := range claim.Citations {
			if !allowed[cite] {
				return domain.Record{}, errors.New("unknown_citation")
			}
		}
	}
	for _, term := range k.Terms {
		for _, cite := range term.Citations {
			if !allowed[cite] {
				return domain.Record{}, errors.New("unknown_citation")
			}
		}
	}
	k.Units = oldK.Units
	k.Images = oldK.Images
	// Issues are produced by parsing; the user may only mark existing ones resolved.
	issues := make([]domain.Issue, len(oldK.Issues))
	copy(issues, oldK.Issues)
	for i := range issues {
		if i < len(k.Issues) && k.Issues[i].Code == issues[i].Code && k.Issues[i].Citation == issues[i].Citation && k.Issues[i].Detail == issues[i].Detail {
			issues[i].Resolved = k.Issues[i].Resolved
		}
	}
	k.Issues = issues
	k.Edits = oldK.Edits + 1
	k.Author, k.Language, k.AIError = oldK.Author, oldK.Language, oldK.AIError
	r, err := a.Store.Create(ctx, domain.Record{Owner: owner, Kind: "knowledge", Parent: paperID, Project: paper.Project, Paper: paperID, Version: old.Version + 1, Data: domain.Encode(k)})
	if err != nil {
		return r, err
	}
	_, err = set[domain.Paper](ctx, a, owner, paperID, paper.Version, func(p *domain.Paper) error {
		p.KnowledgeID = r.ID
		return nil
	})
	if err == nil {
		a.Store.Event(ctx, owner, paper.Project, paperID, "knowledge_revision", map[string]any{"previous": old.ID, "current": r.ID, "edits": k.Edits})
	}
	return r, err
}

// ImportTemplate stores a PowerPoint template (.pptx or .potx) and reads its
// layouts, colours and fonts so decks can be built on it.
func (a *App) ImportTemplate(ctx context.Context, owner, name, filename string, b []byte) (domain.Record, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	word := ext == ".docx" || ext == ".dotx"
	if ext != ".pptx" && ext != ".potx" && !word {
		return domain.Record{}, errors.New("template_must_be_office")
	}
	if len(b) < 4 || len(b) > 64<<20 || string(b[:2]) != "PK" {
		return domain.Record{}, errors.New("invalid_office_file")
	}
	rel, _, err := a.Store.Blob(b, ext)
	if err != nil {
		return domain.Record{}, err
	}
	file, err := a.Store.Path(rel)
	if err != nil {
		return domain.Record{}, err
	}
	var t domain.Template
	if word {
		t, err = artifacts.InspectWordTemplate(ctx, a.Store, file)
	} else {
		t, err = artifacts.InspectTemplate(ctx, a.Store, file)
		t.Kind = "pptx"
	}
	if err != nil {
		return domain.Record{}, err
	}
	t.File = rel
	t.Name = strings.TrimSpace(name)
	if t.Name == "" {
		t.Name = strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	}
	return a.create(ctx, owner, "template", "", "", "", t)
}
func (a *App) SaveProvider(ctx context.Context, owner, id string, p domain.Provider, key string) (domain.Record, error) {
	if p.Kind != "codex" && p.Kind != "claude-cli" && p.Kind != "openai" && p.Kind != "anthropic" && p.Kind != "source-extract" {
		return domain.Record{}, errors.New("unsupported_provider")
	}
	if p.Kind == "openai" || p.Kind == "anthropic" {
		p.Paid = true
	}
	p.MaxOutput = 0
	p.InputPrice = nil
	p.OutputPrice = nil
	var r domain.Record
	var err error
	if id == "" {
		r, err = a.create(ctx, owner, "provider", "", "", "", p)
	} else {
		_, old, e := get[domain.Provider](ctx, a, owner, id, "provider")
		if e != nil {
			return r, e
		}
		if p.Kind != old.Kind {
			return r, errors.New("provider_kind_immutable")
		}
		p.HasCredential = old.HasCredential
		r, err = set[domain.Provider](ctx, a, owner, id, 0, func(old *domain.Provider) error {
			*old = p
			return nil
		})
	}
	if err != nil {
		return r, err
	}
	if key != "" {
		if err = a.Providers.SetSecret(owner, r.ID, key); err != nil {
			return r, err
		}
		r, err = set[domain.Provider](ctx, a, owner, r.ID, 0, func(p *domain.Provider) error {
			p.HasCredential = true
			return nil
		})
	}
	return r, err
}
func (a *App) DeleteProject(ctx context.Context, owner, id string) error {
	if _, err := a.Store.Get(ctx, owner, id, "project"); err != nil {
		return err
	}
	tasks, err := a.Store.List(ctx, owner, store.Filter{Kind: "task", Project: id})
	if err != nil {
		return err
	}
	for _, r := range tasks {
		t, _ := domain.Decode[domain.Task](r.Data)
		if t.Status == "running" || t.Status == "queued" {
			return errors.New("cancel_active_tasks_before_delete")
		}
	}
	return a.Store.Delete(ctx, owner, id)
}
func (a *App) Summary(ctx context.Context, owner, project string) (map[string]any, error) {
	out := map[string]any{}
	if project != "" {
		p, err := a.Store.Get(ctx, owner, project, "project")
		if err != nil {
			return nil, err
		}
		out["project"] = p
	}
	for _, kind := range []string{"paper", "source", "knowledge", "request", "preview", "task", "step", "artifact", "folder", "event"} {
		r, err := a.Store.List(ctx, owner, store.Filter{Kind: kind, Project: project})
		if err != nil {
			return nil, err
		}
		out[kind] = r
	}
	for _, kind := range []string{"profile", "profile_version", "template", "provider", "preference"} {
		r, err := a.Store.List(ctx, owner, store.Filter{Kind: kind})
		if err != nil {
			return nil, err
		}
		out[kind] = r
	}
	return out, nil
}
func kindError(kind string) error { return fmt.Errorf("invalid_%s", kind) }
