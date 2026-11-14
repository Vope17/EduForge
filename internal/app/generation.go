package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"eduforge/internal/artifacts"
	"eduforge/internal/documents"
	"eduforge/internal/domain"
	"eduforge/internal/providers"
	"eduforge/internal/research"
	"eduforge/internal/store"
	styles "eduforge/internal/style"
)

func (a *App) resolveStyle(ctx context.Context, owner, project, profileID string, selected []string, overrides map[string]domain.Style) (domain.StyleSnapshot, error) {
	_, p, err := get[domain.Project](ctx, a, owner, project, "project")
	if err != nil {
		return domain.StyleSnapshot{}, err
	}
	var v domain.ProfileVersion
	versionID := ""
	if profileID != "" {
		_, profile, err := get[domain.Profile](ctx, a, owner, profileID, "profile")
		if err != nil {
			return domain.StyleSnapshot{}, err
		}
		if !profile.Enabled {
			return domain.StyleSnapshot{}, errors.New("profile_disabled")
		}
		r, version, e := get[domain.ProfileVersion](ctx, a, owner, profile.Current, "profile_version")
		if e != nil {
			return domain.StyleSnapshot{}, e
		}
		if !version.Confirmed || r.Parent != profileID {
			return domain.StyleSnapshot{}, errors.New("profile_not_confirmed")
		}
		v = version
		versionID = r.ID
	}
	snapshot, err := styles.Snapshot(selected, v, p.Style, overrides)
	snapshot.ProfileVersionID = versionID
	return snapshot, err
}
func (a *App) CreateRequest(ctx context.Context, owner, project string, req domain.Request) (domain.Record, error) {
	if !domain.Language(req.UILanguage) {
		return domain.Record{}, errors.New("invalid_language")
	}
	for kind, lang := range req.Languages {
		if kind != "A" && kind != "B" && kind != "C" {
			return domain.Record{}, errors.New("invalid_output")
		}
		if lang != "" && !domain.Language(lang) {
			return domain.Record{}, errors.New("invalid_language")
		}
	}
	if len(req.PaperIDs) == 0 {
		return domain.Record{}, errors.New("select_paper")
	}
	req.Unlimited = true
	req.AllowPaid = true
	req.Budget = nil
	req.Targets = nil
	targets, err := a.generationTargets(ctx, owner, project, req)
	if err != nil {
		return domain.Record{}, err
	}
	req.Targets = targets
	if req.UsePaperSettings {
		req.Selected = nil
		for _, kind := range []string{"A", "B", "C"} {
			for _, target := range targets {
				if slices.Contains(target.Settings.Selected, kind) {
					req.Selected = append(req.Selected, kind)
					break
				}
			}
		}
	}
	if req.Combined {
		if len(req.PaperIDs) < 2 {
			return domain.Record{}, errors.New("related_group_requires_multiple_studies")
		}
		pairs, e := a.Pairs(ctx, owner, project)
		if e != nil {
			return domain.Record{}, e
		}
		selected := map[string]bool{}
		for _, id := range req.PaperIDs {
			selected[id] = true
		}
		connected := map[string]bool{req.PaperIDs[0]: true}
		for pass := 0; pass < len(req.PaperIDs); pass++ {
			for _, pair := range pairs {
				if pair.Related && selected[pair.A] && selected[pair.B] && (connected[pair.A] || connected[pair.B]) {
					connected[pair.A] = true
					connected[pair.B] = true
				}
			}
		}
		if len(connected) != len(selected) {
			return domain.Record{}, errors.New("confirm_related_group_first")
		}
		if req.UsePaperSettings {
			return domain.Record{}, errors.New("combined_uses_batch_settings")
		}
		if !slices.Contains(req.Selected, "A") && !slices.Contains(req.Selected, "B") {
			return domain.Record{}, errors.New("combined_requires_reader_or_presentation")
		}
	}
	refs, err := a.ProviderRefs(ctx, owner)
	if err != nil {
		return domain.Record{}, err
	}
	req.Providers = refs
	for _, style := range req.Overrides {
		if err := a.validateStyle(ctx, owner, style); err != nil {
			return domain.Record{}, err
		}
	}
	req.Style, err = a.resolveStyle(ctx, owner, project, req.ProfileID, req.Selected, req.Overrides)
	if err != nil {
		return domain.Record{}, err
	}
	req.Status = "preparing"
	req.Spent = 0
	req.PreviewIDs = []string{}
	req.Settings = NormalizeSettings(req.Settings)
	req.Started = domain.Now()
	return a.create(ctx, owner, "request", project, project, "", req)
}
func (a *App) generationTargets(ctx context.Context, owner, project string, req domain.Request) ([]domain.GenerationTarget, error) {
	if len(req.Targets) != 0 {
		return req.Targets, nil
	}
	targets := make([]domain.GenerationTarget, 0, len(req.PaperIDs))
	seen := make(map[string]bool, len(req.PaperIDs))
	for _, id := range req.PaperIDs {
		if seen[id] {
			return nil, errors.New("duplicate_paper")
		}
		seen[id] = true
		r, p, err := get[domain.Paper](ctx, a, owner, id, "paper")
		if err != nil || r.Project != project {
			return nil, domain.ErrNotFound
		}
		if p.KnowledgeID == "" {
			return nil, errors.New("parse_before_generation")
		}
		_, k, err := get[domain.Knowledge](ctx, a, owner, p.KnowledgeID, "knowledge")
		if err != nil {
			return nil, err
		}
		if !k.Confirmed {
			return nil, errors.New("confirm_knowledge_first")
		}
		settings := req.Settings
		settings.Selected = req.Selected
		settings.Languages = maps.Clone(req.Settings.Languages)
		if settings.Languages == nil {
			settings.Languages = make(map[string]string)
		}
		for kind, lang := range req.Languages {
			if lang != "" {
				settings.Languages[kind] = lang
			}
		}
		if req.UsePaperSettings {
			settings = p.Settings
			if len(settings.Selected) == 0 {
				return nil, errors.New("paper_output_settings_required")
			}
		}
		settings = NormalizeSettings(settings)
		if err := validKinds(settings.Selected); err != nil {
			return nil, err
		}
		if err := validateOutputSettings(settings); err != nil {
			return nil, err
		}
		target := domain.GenerationTarget{PaperID: id, KnowledgeID: p.KnowledgeID, Settings: settings}
		if slices.Contains(settings.Selected, "B") && settings.TemplateID != "" {
			t, err := a.templateOf(ctx, owner, settings.TemplateID, "pptx")
			if err != nil {
				return nil, err
			}
			target.TemplateStyle = artifacts.TemplateStyle(t)
		}
		if slices.Contains(settings.Selected, "A") && settings.ReaderTemplateID != "" {
			t, err := a.templateOf(ctx, owner, settings.ReaderTemplateID, "docx")
			if err != nil {
				return nil, err
			}
			target.ReaderTemplateStyle = artifacts.TemplateStyle(t)
		}
		targets = append(targets, target)
	}
	return targets, nil
}

// template returns an imported template.
func (a *App) template(ctx context.Context, owner, id string) (domain.Template, error) {
	_, t, err := get[domain.Template](ctx, a, owner, id, "template")
	if err != nil {
		return t, err
	}
	if !t.Confirmed {
		return t, errors.New("confirm_template_first")
	}
	return t, nil
}

// templateOf returns an imported template of the given kind: pptx for
// slides, docx for readers.
func (a *App) templateOf(ctx context.Context, owner, id, kind string) (domain.Template, error) {
	t, err := a.template(ctx, owner, id)
	if err != nil {
		return t, err
	}
	if firstKind(t.Kind) != kind || t.File == "" {
		return t, errors.New("template_kind_mismatch")
	}
	return t, nil
}

func firstKind(kind string) string {
	if kind == "" {
		return "pptx"
	}
	return kind
}

func targetStyle(req domain.Request, target domain.GenerationTarget, kind string) domain.StyleSnapshot {
	snapshot := req.Style
	resolved := maps.Clone(req.Style.Resolved[kind])
	if resolved == nil {
		resolved = styles.Defaults(kind)
	}
	templateStyle := map[string]domain.Style{"A": target.ReaderTemplateStyle, "B": target.TemplateStyle}[kind]
	for key, value := range templateStyle {
		if _, explicit := req.Overrides[kind][key]; !explicit {
			resolved[key] = value
		}
	}
	snapshot.Resolved = map[string]domain.Style{kind: resolved}
	return snapshot
}

func language(req domain.Request, target domain.GenerationTarget, kind string) string {
	if v := target.Settings.Languages[kind]; domain.Language(v) {
		return v
	}
	return req.UILanguage
}

func (a *App) StartRequest(ctx context.Context, owner, requestID, ui string, useDefaults bool) ([]domain.Record, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, req, err := get[domain.Request](ctx, a, owner, requestID, "request")
	if err != nil {
		return nil, err
	}
	if req.Status != "preparing" {
		return nil, errors.New("request_already_started_or_paused")
	}
	if !domain.Language(req.UILanguage) {
		if !domain.Language(ui) {
			return nil, errors.New("invalid_language")
		}
		req.UILanguage = ui
	}
	targets, err := a.generationTargets(ctx, owner, r.Project, req)
	if err != nil {
		return nil, err
	}
	out := []domain.Record{}
	known := []string{}
	history, err := a.Store.List(ctx, owner, store.Filter{Kind: "reading"})
	if err != nil {
		return nil, err
	}
	for _, item := range history {
		h, _ := domain.Decode[domain.Reading](item.Data)
		known = append(known, h.Terms...)
	}
	for _, target := range targets {
		for _, kind := range target.Settings.Selected {
			total := 6
			if kind == "C" {
				total = 1
			}
			t := domain.Task{RequestID: requestID, Type: kind, Status: "queued", Stage: "generate", Language: language(req, target, kind), KnowledgeID: target.KnowledgeID, Settings: target.Settings, Style: targetStyle(req, target, kind), Total: total}
			if kind == "A" {
				t.ReadTerms = known
			}
			out = append(out, domain.Record{Project: r.Project, Paper: target.PaperID, Data: domain.Encode(t)})
		}
	}
	if req.Combined {
		ids := []string{}
		for _, target := range targets {
			ids = append(ids, target.KnowledgeID)
		}
		for _, kind := range req.Selected {
			if kind == "C" {
				continue
			}
			target := targets[0]
			task := domain.Task{RequestID: requestID, Type: kind, Status: "queued", Stage: "generate", Language: language(req, target, kind), KnowledgeID: ids[0], RelatedKnowledge: ids[1:], GroupedPapers: req.PaperIDs, Settings: target.Settings, Style: targetStyle(req, target, kind), Total: 6}
			if kind == "A" {
				task.ReadTerms = known
			}
			out = append(out, domain.Record{Project: r.Project, Paper: req.PaperIDs[0], Data: domain.Encode(task)})
		}
	}
	req.Status = "running"
	req.Targets = targets
	return a.Store.CommitTasks(ctx, owner, requestID, r.Version, domain.Encode(req), out)
}

func (a *App) ControlTask(ctx context.Context, owner, id, action string) (domain.Record, error) {
	r, t, err := get[domain.Task](ctx, a, owner, id, "task")
	if err != nil {
		return r, err
	}
	if t.Status == "completed" || t.Status == "cancelled" {
		return r, errors.New("task_is_terminal")
	}
	status := ""
	switch action {
	case "pause":
		status = "paused"
	case "resume", "retry":
		status = "queued"
	case "cancel":
		status = "cancelled"
	default:
		return r, errors.New("invalid_task_action")
	}
	if status == "queued" && t.RequestID != "" {
		_, req, err := get[domain.Request](ctx, a, owner, t.RequestID, "request")
		if err != nil {
			return r, err
		}
		if req.Status == "cancelled" {
			return r, errors.New("request_cancelled")
		}
		if strings.HasPrefix(t.Type, "preview:") && req.Status == "running" {
			return r, errors.New("request_already_started")
		}
	}
	return set[domain.Task](ctx, a, owner, id, r.Version, func(t *domain.Task) error {
		t.Status = status
		t.Error = ""
		return nil
	})
}
func (a *App) ControlRequest(ctx context.Context, owner, id, action string, budget *float64, unlimited bool, refreshProviders bool) (domain.Record, error) {
	r, req, err := get[domain.Request](ctx, a, owner, id, "request")
	if err != nil {
		return r, err
	}
	if req.Status == "cancelled" {
		return r, errors.New("request_cancelled")
	}
	status := ""
	switch action {
	case "pause":
		status = "paused"
	case "cancel":
		status = "cancelled"
	case "resume":
		status = "preparing"
		allTasks, e := a.Store.List(ctx, owner, store.Filter{Kind: "task", Parent: id})
		if e != nil {
			return r, e
		}
		for _, row := range allTasks {
			task, _ := domain.Decode[domain.Task](row.Data)
			if !strings.HasPrefix(task.Type, "preview:") {
				status = "running"
				break
			}
		}
	default:
		return r, errors.New("invalid_request_action")
	}
	refs := req.Providers
	if refreshProviders {
		refs, err = a.ProviderRefs(ctx, owner)
		if err != nil {
			return r, err
		}
	}
	r, err = set[domain.Request](ctx, a, owner, id, r.Version, func(v *domain.Request) error {
		v.Status = status
		v.Providers = refs
		v.Budget = nil
		v.Unlimited = true
		v.AllowPaid = true

		return nil
	})
	if err != nil {
		return r, err
	}
	tasks, err := a.Store.List(ctx, owner, store.Filter{Kind: "task", Parent: id})
	if err != nil {
		return r, err
	}
	for _, task := range tasks {
		t, _ := domain.Decode[domain.Task](task.Data)
		if t.Status == "completed" || t.Status == "cancelled" {
			continue
		}
		if status == "running" && strings.HasPrefix(t.Type, "preview:") {
			continue
		}
		next := status
		if status == "running" || status == "preparing" {
			next = "queued"
			if t.Status == "running" {
				continue
			}
		}
		_, _ = set[domain.Task](ctx, a, owner, task.ID, 0, func(v *domain.Task) error {
			v.Status = next
			v.Error = ""
			return nil
		})
	}
	return r, nil
}

func (a *App) Start(ctx context.Context) {
	ctx, a.cancel = context.WithCancel(ctx)
	a.recover(ctx)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.tick(ctx)
			}
		}
	}()
}
func (a *App) Stop() {
	if a.cancel != nil {
		a.cancel()
	}
	a.wg.Wait()
}
func (a *App) recover(ctx context.Context) {
	owners, err := a.Store.Users(ctx)
	if err != nil {
		return
	}
	for _, owner := range owners {
		tasks, _ := a.Store.List(ctx, owner, store.Filter{Kind: "task"})
		for _, r := range tasks {
			t, _ := domain.Decode[domain.Task](r.Data)
			if t.Status == "running" {
				_, _ = set[domain.Task](ctx, a, owner, r.ID, 0, func(t *domain.Task) error {
					t.Status = "queued"
					t.Stage = "recovering"
					return nil
				})
			}
		}
	}
}
func (a *App) tick(ctx context.Context) {
	owners, err := a.Store.Users(ctx)
	if err != nil {
		return
	}
	for _, owner := range owners {
		a.pollMu.Lock()
		if !a.polling {
			a.polling = true
			a.wg.Add(1)
			go func() {
				defer a.wg.Done()
				defer func() {
					a.pollMu.Lock()
					a.polling = false
					a.pollMu.Unlock()
				}()
				for _, user := range owners {
					a.pollRuns(ctx, user)
				}
			}()
		}
		a.pollMu.Unlock()
		tasks, err := a.Store.List(ctx, owner, store.Filter{Kind: "task"})
		if err != nil {
			continue
		}
		for _, r := range tasks {
			t, _ := domain.Decode[domain.Task](r.Data)
			if t.Status != "queued" {
				continue
			}
			a.mu.Lock()
			if len(a.running) >= 3 || a.running[r.ID] {
				a.mu.Unlock()
				continue
			}
			a.running[r.ID] = true
			a.mu.Unlock()
			a.wg.Add(1)
			go func(r domain.Record) {
				defer a.wg.Done()
				defer func() {
					a.mu.Lock()
					delete(a.running, r.ID)
					a.mu.Unlock()
				}()
				err := a.process(ctx, r.Owner, r.ID)
				if err != nil && !errors.Is(err, context.Canceled) {
					status := "failed"
					if errors.Is(err, domain.ErrPaused) {
						status = "paused"
					}
					_, _ = set[domain.Task](context.Background(), a, r.Owner, r.ID, 0, func(t *domain.Task) error {
						if t.Status != "cancelled" && t.Status != "paused" {
							t.Status = status
							t.Error = err.Error()
						}
						return nil
					})
					a.updateRequestStatus(context.Background(), r.Owner, t.RequestID)
				}
			}(r)
		}
	}
}
func (a *App) taskCanContinue(ctx context.Context, owner, id string) bool {
	_, t, err := get[domain.Task](ctx, a, owner, id, "task")
	return err == nil && (t.Status == "running" || t.Status == "queued")
}
func (a *App) process(ctx context.Context, owner, id string) error {
	r, t, err := get[domain.Task](ctx, a, owner, id, "task")
	if err != nil {
		return err
	}
	if t.Status != "queued" {
		return nil
	}
	_, err = set[domain.Task](ctx, a, owner, id, r.Version, func(t *domain.Task) error {
		t.Status = "running"
		return nil
	})
	if err != nil {
		return err
	}
	if t.Type == "parse" {
		return a.parseTask(ctx, owner, r)
	}
	_, k, err := get[domain.Knowledge](ctx, a, owner, t.KnowledgeID, "knowledge")
	if err != nil {
		return err
	}
	for _, kid := range t.RelatedKnowledge {
		_, other, e := get[domain.Knowledge](ctx, a, owner, kid, "knowledge")
		if e != nil {
			return e
		}
		k.Title += " / " + other.Title
		k.Units = append(k.Units, other.Units...)
		k.Claims = append(k.Claims, other.Claims...)
		k.Terms = append(k.Terms, other.Terms...)
		k.Images = append(k.Images, other.Images...)
		k.Issues = append(k.Issues, other.Issues...)
	}
	kind := strings.TrimPrefix(t.Type, "preview:")
	preview := strings.HasPrefix(t.Type, "preview:")
	// A reproduction plan (C) is written chapter by chapter, taking the
	// user's saved planning conversation into account.
	var chat *domain.PlanChat
	if kind == "C" && t.Settings.PlanChatID != "" {
		_, c, err := get[domain.PlanChat](ctx, a, owner, t.Settings.PlanChatID, "plan_chat")
		if err != nil {
			return err
		}
		chat = &c
	}
	steps, err := a.Store.List(ctx, owner, store.Filter{Kind: "step", Parent: id})
	if err != nil {
		return err
	}
	done := map[string]domain.Step{}
	for _, s := range steps {
		step, _ := domain.Decode[domain.Step](s.Data)
		done[step.Key] = step
	}
	var sections []domain.Section
	mode := "model"
	if kind == "B" && !preview {
		// A deck follows the paper: planned first, then written in batches.
		var finished bool
		sections, mode, finished, err = a.writeDeck(ctx, owner, id, r, t, k, done)
		if err != nil || !finished {
			return err
		}
	} else {
		// Readers and decks are written section by section in SectionOrder
		// (related work only when the paper discusses it); plans chapter by
		// chapter. A style preview writes the first section only.
		order := []int{}
		if kind == "C" {
			for i := range research.PlanChapters {
				order = append(order, i)
			}
		} else {
			for _, i := range research.SectionOrder {
				if i != research.RelatedSection || research.HasEvidence(k, i) {
					order = append(order, i)
				}
			}
		}
		if preview {
			order = order[:1]
		}
		count := len(order)
		slides := research.SlideCounts(t.Settings.Minutes)
		for _, i := range order {
			key := fmt.Sprintf("section:%d", i)
			if _, ok := done[key]; ok {
				continue
			}
			if !a.taskCanContinue(ctx, owner, id) {
				return nil
			}
			req := research.Request{Knowledge: k, Index: i, Lang: t.Language, Kind: kind, Settings: t.Settings, Style: t.Style.Resolved[kind], Known: t.ReadTerms, Slides: slides[i]}
			if kind == "C" {
				ch := research.PlanChapters[i]
				req.Section = research.Excerpt(k, ch.Excerpt, "deep", t.Language)
				req.Section.Title = ch.TitleEN
				if t.Language == "zh-TW" {
					req.Section.Title = ch.TitleZH
				}
				req.Index, req.Known = -1, nil
			} else {
				req.Section = research.Excerpt(k, i, t.Settings.Depth, t.Language)
			}
			passages, pages := req.Evidence()
			prompt := req.Prompt(passages, pages)
			if kind == "C" {
				prompt = research.PlanPrompt(prompt, research.PlanChapters[i], chat)
			}
			images := []string{}
			for _, u := range pages {
				if path, err := a.Store.Path(u.Preview); err == nil {
					images = append(images, path)
				}
			}
			result, err := a.Providers.Generate(providers.WithImages(ctx, images), owner, t.RequestID, prompt, req.Section)
			if err != nil {
				return err
			}
			step := domain.Step{TaskID: id, Key: key, Content: result.Section, Provider: result.Provider, Model: result.Model, Language: t.Language, Cost: result.Cost, Mode: result.Mode}
			if _, err = a.create(ctx, owner, "step", id, r.Project, r.Paper, step); err != nil {
				return err
			}
			done[key] = step
			_, _ = set[domain.Task](ctx, a, owner, id, 0, func(v *domain.Task) error {
				v.Completed = len(done)
				v.Total = count
				return nil
			})
		}
		if !a.taskCanContinue(ctx, owner, id) {
			return nil
		}
		sections = []domain.Section{}
		shown := map[string]bool{}
		for _, i := range order {
			step := done[fmt.Sprintf("section:%d", i)]
			if len(step.Content.Slides) > 0 {
				// A deck topic written as several slides, each showing one figure.
				sections = append(sections, slideFigures(step.Content.Slides, k.Images, shown)...)
			} else {
				step.Content.Slides = nil
				sections = append(sections, step.Content)
			}
			if step.Mode == "source-extract" {
				mode = "source-extract"
			}
		}
	}
	// Related research is the paper's own related work (a section above);
	// only a reproduction plan adds comparisons with related studies in the
	// library.
	related := append([]string{}, t.RelatedKnowledge...)
	if !preview && kind == "C" {
		related = append(related, a.addComparisons(ctx, owner, r.Project, r.Paper, t.KnowledgeID, &sections, t.Language)...)
	}
	art := domain.Artifact{GroupedPapers: t.GroupedPapers, RelatedKnowledge: related, Type: kind, Language: t.Language, KnowledgeID: t.KnowledgeID, RequestID: t.RequestID, TaskID: id, Style: t.Style, Settings: t.Settings, Sections: sections, Mode: mode}
	support, err := a.ArtifactKnowledge(ctx, owner, art)
	if err != nil {
		return err
	}
	if !preview {
		switch kind {
		case "A":
			// Each section shows the figures its text mentions (placed under the
			// paragraph that mentions them); record them so the editor can
			// change them.
			for i, paths := range artifacts.MentionedFigures(art, support) {
				art.Sections[i].Figures = paths
			}
		case "C":
			for i := range art.Sections {
				art.Sections[i].Figures = []string{} // plans are text only
			}
		case "B":
			art.Sections = artifacts.BuildDeck(art.Sections, support, k.Title, t.Language, func(path string) float64 { return artifacts.ImageAspect(a.Store, path) }, artifacts.GeometryOf(art.Style.Resolved["B"]))
		}
	}
	if kind == "B" {
		art.EstimatedMinutes = research.PresentationMinutes(art.Sections)
	}
	art.Check = research.Validate(art, support)
	return a.finishArtifact(ctx, owner, r, art, preview)
}

// writeDeck writes a deck that follows the paper: a model plans the talk
// from the whole paper (step "outline"), then writes the slides a batch at
// a time from their pages and page images (steps "slides:<n>"). It reports
// whether the task got to the end (false when paused or cancelled).
func (a *App) writeDeck(ctx context.Context, owner, id string, r domain.Record, t domain.Task, k domain.Knowledge, done map[string]domain.Step) ([]domain.Section, string, bool, error) {
	save := func(key string, step domain.Step, total int) error {
		step.TaskID, step.Key, step.Language = id, key, t.Language
		if _, err := a.create(ctx, owner, "step", id, r.Project, r.Paper, step); err != nil {
			return err
		}
		done[key] = step
		_, _ = set[domain.Task](ctx, a, owner, id, 0, func(v *domain.Task) error {
			v.Completed = len(done)
			v.Total = total
			return nil
		})
		return nil
	}
	if _, ok := done["outline"]; !ok {
		if !a.taskCanContinue(ctx, owner, id) {
			return nil, "", false, nil
		}
		step := domain.Step{Mode: "model"}
		var outline []domain.Section
		_, request, err := get[domain.Request](ctx, a, owner, t.RequestID, "request")
		if err != nil {
			return nil, "", false, err
		}
		_, provider, err := a.Providers.Complete(ctx, owner, request.Providers, research.OutlinePrompt(k, t.Settings, t.Language), func(output string) error {
			parsed, err := research.ParseOutline(output, k)
			if err == nil {
				outline = parsed
			}
			return err
		})
		switch {
		case err == nil:
			step.Provider = provider
		case err.Error() == "model_connection_required":
			// No model: one slide per section of the shared knowledge.
			outline, step.Mode = research.FallbackOutline(k, t.Settings.Depth, t.Language), "source-extract"
		case ctx.Err() != nil:
			return nil, "", false, ctx.Err()
		default:
			return nil, "", false, domain.ErrPaused
		}
		step.Content = domain.Section{Title: "outline", Body: "-", Citations: []string{}, Slides: outline}
		if err = save("outline", step, 1+len(research.Batches(outline))); err != nil {
			return nil, "", false, err
		}
	}
	outline := done["outline"].Content.Slides
	batches := research.Batches(outline)
	total := 1 + len(batches)
	first := 0
	for n, batch := range batches {
		key := fmt.Sprintf("slides:%d", n)
		req := research.SlidesRequest{Knowledge: k, Outline: outline, First: first, Count: len(batch), Lang: t.Language, Settings: t.Settings, Style: t.Style.Resolved["B"]}
		first += len(batch)
		if _, ok := done[key]; ok {
			continue
		}
		if !a.taskCanContinue(ctx, owner, id) {
			return nil, "", false, nil
		}
		passages, pages := req.Evidence()
		images := []string{}
		for _, u := range pages {
			if path, err := a.Store.Path(u.Preview); err == nil {
				images = append(images, path)
			}
		}
		// Without a model the planned slides are kept as written.
		fallback := domain.Section{Title: "slides", Body: "-", Citations: []string{}, Slides: []domain.Section{}}
		for _, s := range batch {
			s.Equation, s.Figures = "", nil
			fallback.Slides = append(fallback.Slides, s)
		}
		result, err := a.Providers.Generate(providers.WithImages(ctx, images), owner, t.RequestID, req.Prompt(passages, pages), fallback)
		if err != nil {
			return nil, "", false, err
		}
		if err = save(key, domain.Step{Content: result.Section, Provider: result.Provider, Model: result.Model, Cost: result.Cost, Mode: result.Mode}, total); err != nil {
			return nil, "", false, err
		}
	}
	if !a.taskCanContinue(ctx, owner, id) {
		return nil, "", false, nil
	}
	sections := []domain.Section{}
	mode := done["outline"].Mode
	shown := map[string]bool{}
	for n := range batches {
		step := done[fmt.Sprintf("slides:%d", n)]
		if step.Mode == "source-extract" {
			mode = "source-extract"
		}
		slides := step.Content.Slides
		if len(slides) == 0 {
			step.Content.Slides = nil
			slides = []domain.Section{step.Content}
		}
		sections = append(sections, slideFigures(slides, k.Images, shown)...)
	}
	return sections, mode, true, nil
}

// slideFigures gives each slide the figures the model chose for it (up to
// two, by label such as "Figure 9"), else the first figure not shown yet
// from the pages it cites, or none. A figure is shown once in the deck, so
// a topic's figures do not become slides of their own.
func slideFigures(slides []domain.Section, images []domain.Image, shown map[string]bool) []domain.Section {
	labels := artifacts.FigureLabels(images)
	for i := range slides {
		chosen := []string{}
		for _, label := range slides[i].Figures {
			if img, ok := labels[artifacts.LabelKey(label)]; ok && !shown[img.Path] && len(chosen) < 2 {
				chosen = append(chosen, img.Path)
				shown[img.Path] = true
			}
		}
	pick:
		for _, cite := range slides[i].Citations {
			if len(chosen) > 0 {
				break
			}
			for _, img := range images {
				if img.Citation == cite && !shown[img.Path] {
					chosen = append(chosen, img.Path)
					shown[img.Path] = true
					break pick
				}
			}
		}
		slides[i].Figures = chosen
	}
	return slides
}

func (a *App) finishArtifact(ctx context.Context, owner string, task domain.Record, art domain.Artifact, preview bool) error {
	kind := "artifact"
	if preview {
		kind = "preview"
	}
	items, err := a.Store.List(ctx, owner, store.Filter{Kind: kind, Paper: task.Paper})
	if err != nil {
		return err
	}
	var result domain.Record
	for _, r := range items {
		v, _ := domain.Decode[domain.Artifact](r.Data)
		if v.TaskID == task.ID && v.Previous == "" {
			result = r
			break
		}
	}
	if result.ID == "" {
		result, err = a.create(ctx, owner, kind, task.ID, task.Project, task.Paper, art)
		if err != nil {
			return err
		}
	}
	_, err = set[domain.Task](ctx, a, owner, task.ID, 0, func(t *domain.Task) error {
		if t.Status == "cancelled" || t.Status == "paused" {
			return nil
		}
		t.Status = "completed"
		t.ResultID = result.ID
		t.Completed = t.Total
		return nil
	})
	if err == nil {
		a.updateRequestStatus(ctx, owner, art.RequestID)
	}
	return err
}

func (a *App) updateRequestStatus(ctx context.Context, owner, id string) {
	if id == "" {
		return
	}
	tasks, err := a.Store.List(ctx, owner, store.Filter{Kind: "task", Parent: id})
	if err != nil {
		return
	}
	formal, failed := 0, false
	for _, row := range tasks {
		task, err := domain.Decode[domain.Task](row.Data)
		if err != nil || strings.HasPrefix(task.Type, "preview:") {
			continue
		}
		formal++
		switch task.Status {
		case "failed", "cancelled":
			failed = true
		case "completed":
		default:
			return
		}
	}
	if formal == 0 {
		return
	}
	_, _ = set[domain.Request](ctx, a, owner, id, 0, func(req *domain.Request) error {
		if req.Status == "cancelled" || req.Status == "paused" {
			return nil
		}
		req.Status = "completed"
		if failed {
			req.Status = "failed"
		}
		return nil
	})
}
func (a *App) parseTask(ctx context.Context, owner string, task domain.Record) error {
	sources, err := a.Store.List(ctx, owner, store.Filter{Kind: "source", Paper: task.Paper})
	if err != nil {
		return err
	}
	completed := 0
	for _, r := range sources {
		src, _ := domain.Decode[domain.Source](r.Data)
		if src.Role != "research" {
			continue
		}
		if !a.taskCanContinue(ctx, owner, task.ID) {
			return nil
		}
		if documents.NeedsParse(src) {
			p, e := documents.Parse(ctx, a.Store, r)
			_, err = set[domain.Source](ctx, a, owner, r.ID, 0, func(v *domain.Source) error {
				if e != nil {
					v.Error = e.Error()
				} else {
					v.Parsed = &p
					v.Error = ""
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		completed++
		_, _ = set[domain.Task](ctx, a, owner, task.ID, 0, func(t *domain.Task) error {
			t.Completed = completed
			return nil
		})
	}
	sources, err = a.Store.List(ctx, owner, store.Filter{Kind: "source", Paper: task.Paper})
	if err != nil {
		return err
	}
	_, p, err := get[domain.Paper](ctx, a, owner, task.Paper, "paper")
	if err != nil {
		return err
	}
	k := research.Extract(p.Title, sources)
	if len(k.Units) == 0 {
		return errors.New("all_research_sources_failed")
	}
	// With a model connected, it writes the key points; otherwise (or if it
	// fails) the source excerpts stay and the reason is kept.
	if a.taskCanContinue(ctx, owner, task.ID) {
		_, _ = set[domain.Task](ctx, a, owner, task.ID, 0, func(t *domain.Task) error {
			t.Stage = "knowledge"
			return nil
		})
		if written, err := a.writeKnowledge(ctx, owner, k); err == nil {
			k = written
		} else if ctx.Err() != nil {
			return ctx.Err()
		} else {
			k.AIError = err.Error()
		}
	}
	if !a.taskCanContinue(ctx, owner, task.ID) {
		return nil
	}
	r, err := a.create(ctx, owner, "knowledge", task.Paper, task.Project, task.Paper, k)
	if err != nil {
		return err
	}
	_, err = set[domain.Paper](ctx, a, owner, task.Paper, 0, func(p *domain.Paper) error {
		p.KnowledgeID = r.ID
		return nil
	})
	if err != nil {
		return err
	}
	_, err = set[domain.Task](ctx, a, owner, task.ID, 0, func(t *domain.Task) error {
		t.Status = "completed"
		t.ResultID = r.ID
		t.Completed = t.Total
		return nil
	})
	return err
}
func (a *App) addComparisons(ctx context.Context, owner, project, paper, knowledgeID string, sections *[]domain.Section, lang string) []string {
	ids := []string{}
	pairs, err := a.Pairs(ctx, owner, project)
	if err != nil {
		return ids
	}
	for _, pair := range pairs {
		if !pair.Related || (pair.A != paper && pair.B != paper) {
			continue
		}
		title := "Related research comparison"
		note := "Compare concepts. Dataset, split, metric and evaluation settings must match before comparing values."
		if lang == "zh-TW" {
			title = "相關研究比較"
			note = "可比較概念。資料集、切分、指標與評估條件相符後，再核對數值。"
		}
		if pair.Numeric {
			note = "The recorded comparison conditions match. The excerpts below preserve each source's methods, results and limitations."
			if lang == "zh-TW" {
				note = "已記錄相符的比較條件。以下分列各研究來源的方法、結果與限制。"
			}
		}
		if pair.Incomplete {
			note = "Comparison evidence is incomplete because a source is missing or failed."
			if lang == "zh-TW" {
				note = "來源缺失或解析失敗，比較證據尚不完整。"
			}
		}
		parts := []string{note}
		cites := []string{}
		conditions := []domain.Conditions{}
		for _, paperID := range []string{pair.A, pair.B} {
			_, study, err := get[domain.Paper](ctx, a, owner, paperID, "paper")
			if err != nil || study.KnowledgeID == "" {
				continue
			}
			if paperID == paper {
				study.KnowledgeID = knowledgeID
			}
			_, knowledge, err := get[domain.Knowledge](ctx, a, owner, study.KnowledgeID, "knowledge")
			if err != nil {
				continue
			}
			if paperID != paper {
				ids = append(ids, study.KnowledgeID)
			}
			conditions = append(conditions, knowledge.Conditions)
			parts = append(parts, knowledge.Title)
			labels := map[int]string{2: "Method", 4: "Results", 5: "Limitations"}
			if lang == "zh-TW" {
				labels = map[int]string{2: "方法", 4: "結果", 5: "限制"}
			}
			for _, section := range []int{2, 4, 5} {
				for _, claim := range knowledge.Claims {
					if claim.Section != section {
						continue
					}
					parts = append(parts, labels[section]+" · "+research.Clip(claim.Text, 650))
					for _, cite := range claim.Citations {
						if !slices.Contains(cites, cite) {
							cites = append(cites, cite)
						}
					}
					break
				}
			}
		}
		if len(conditions) == 2 {
			pair.Differences = research.Compare(conditions[0], conditions[1])
			if len(pair.Differences) > 0 {
				parts[0] = "Evaluation conditions differ or are missing. Compare concepts and review the differences below."
				if lang == "zh-TW" {
					parts[0] = "評估條件不同或不完整。請比較概念，並核對以下條件差異。"
				}
			}
		}
		for _, difference := range pair.Differences {
			parts = append(parts, difference.Field+" · "+difference.A+" / "+difference.B)
		}
		*sections = append(*sections, domain.Section{Title: title, Body: strings.Join(parts, "\n\n"), Citations: cites})
	}
	return ids
}

func (a *App) EditArtifact(ctx context.Context, owner, id string, version int, input domain.Artifact) (domain.Record, error) {
	old, a0, err := get[domain.Artifact](ctx, a, owner, id, "artifact")
	if err != nil {
		return old, err
	}
	if old.Version != version {
		return old, domain.ErrConflict
	}
	k, err := a.ArtifactKnowledge(ctx, owner, a0)
	if err != nil {
		return old, err
	}
	a0.Sections = input.Sections
	if input.EditingSeconds < 0 || math.IsNaN(input.EditingSeconds) || math.IsInf(input.EditingSeconds, 0) {
		return old, errors.New("invalid_editing_time")
	}
	a0.EditingSeconds += input.EditingSeconds
	if err = validateImages(a0, k); err != nil {
		return old, err
	}
	a0.Previous = id
	a0.Edits++
	a0.CreatedFrom = "user_edit"
	if a0.Type == "B" {
		a0.EstimatedMinutes = research.PresentationMinutes(a0.Sections)
		if input.Settings.Minutes >= 1 && input.Settings.Minutes <= 180 {
			a0.Settings.Minutes = input.Settings.Minutes
		}
	}
	a0.Check = research.Validate(a0, k)
	r, err := a.Store.Create(ctx, domain.Record{Owner: owner, Kind: "artifact", Parent: old.Parent, Project: old.Project, Paper: old.Paper, Version: old.Version + 1, Data: domain.Encode(a0)})
	if err == nil {
		a.Store.Event(ctx, owner, old.Project, old.Paper, "artifact_edit", map[string]any{"previous": id, "current": r.ID, "edits": a0.Edits, "editor_elapsed_seconds": input.EditingSeconds})
	}
	return r, err
}
