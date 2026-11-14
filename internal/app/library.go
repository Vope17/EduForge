package app

import (
	"context"
	"errors"
	"strings"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

// DeletePaper removes a study and everything recorded under it: sources,
// knowledge, tasks, outputs, plans and chats go with it through the paper
// foreign key; the reading-history entry is removed explicitly.
func (a *App) DeletePaper(ctx context.Context, owner, id string) error {
	if _, err := a.Store.Get(ctx, owner, id, "paper"); err != nil {
		return err
	}
	tasks, err := a.Store.List(ctx, owner, store.Filter{Kind: "task", Paper: id})
	if err != nil {
		return err
	}
	for _, r := range tasks {
		t, _ := domain.Decode[domain.Task](r.Data)
		if t.Status == "running" || t.Status == "queued" || t.Status == "paused" {
			return errors.New("cancel_active_tasks_before_delete")
		}
	}
	return a.Store.Delete(ctx, owner, id)
}

// outputKey identifies one output across its versions, matching how the
// interface groups them: type, language and the papers it combines.
func outputKey(art domain.Artifact) string {
	return art.Type + "|" + art.Language + "|" + strings.Join(art.GroupedPapers, ",")
}

// DeleteArtifact removes one output version, or with all every version of
// that output (same paper, type, language and grouping).
func (a *App) DeleteArtifact(ctx context.Context, owner, id string, all bool) (int, error) {
	r, art, err := get[domain.Artifact](ctx, a, owner, id, "artifact")
	if err != nil {
		return 0, err
	}
	if !all {
		return 1, a.Store.Delete(ctx, owner, id)
	}
	items, err := a.Store.List(ctx, owner, store.Filter{Kind: "artifact", Project: r.Project, Paper: r.Paper})
	if err != nil {
		return 0, err
	}
	key, deleted := outputKey(art), 0
	for _, item := range items {
		if item.Paper != r.Paper {
			continue
		}
		v, err := domain.Decode[domain.Artifact](item.Data)
		if err != nil || outputKey(v) != key {
			continue
		}
		if err = a.Store.Delete(ctx, owner, item.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func folderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return "", errors.New("folder_name_required")
	}
	return name, nil
}

// CreateFolder adds a library folder to a project.
func (a *App) CreateFolder(ctx context.Context, owner, project, name string) (domain.Record, error) {
	if _, err := a.Store.Get(ctx, owner, project, "project"); err != nil {
		return domain.Record{}, err
	}
	name, err := folderName(name)
	if err != nil {
		return domain.Record{}, err
	}
	return a.create(ctx, owner, "folder", project, project, "", domain.Folder{Name: name})
}

// RenameFolder changes a folder's name.
func (a *App) RenameFolder(ctx context.Context, owner, id, name string) (domain.Record, error) {
	if _, err := a.Store.Get(ctx, owner, id, "folder"); err != nil {
		return domain.Record{}, err
	}
	name, err := folderName(name)
	if err != nil {
		return domain.Record{}, err
	}
	return set[domain.Folder](ctx, a, owner, id, 0, func(f *domain.Folder) error {
		f.Name = name
		return nil
	})
}

// DeleteFolder removes a folder; its studies become unfiled.
func (a *App) DeleteFolder(ctx context.Context, owner, id string) error {
	r, err := a.Store.Get(ctx, owner, id, "folder")
	if err != nil {
		return err
	}
	papers, err := a.Store.List(ctx, owner, store.Filter{Kind: "paper", Project: r.Project})
	if err != nil {
		return err
	}
	for _, item := range papers {
		p, _ := domain.Decode[domain.Paper](item.Data)
		if p.Folder != id {
			continue
		}
		if _, err = a.MovePaper(ctx, owner, item.ID, ""); err != nil {
			return err
		}
	}
	return a.Store.Delete(ctx, owner, id)
}

// MovePaper files a study in a folder of its project, or unfiles it.
func (a *App) MovePaper(ctx context.Context, owner, paperID, folderID string) (domain.Record, error) {
	paper, err := a.Store.Get(ctx, owner, paperID, "paper")
	if err != nil {
		return domain.Record{}, err
	}
	if folderID != "" {
		folder, err := a.Store.Get(ctx, owner, folderID, "folder")
		if err != nil {
			return domain.Record{}, err
		}
		if folder.Project != paper.Project {
			return domain.Record{}, errors.New("folder_project_mismatch")
		}
	}
	return set[domain.Paper](ctx, a, owner, paperID, 0, func(p *domain.Paper) error {
		p.Folder = folderID
		return nil
	})
}
