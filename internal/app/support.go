package app

import (
	"context"
	"strings"

	"eduforge/internal/domain"
)

func (a *App) ArtifactKnowledge(ctx context.Context, owner string, art domain.Artifact) (domain.Knowledge, error) {
	_, k, err := get[domain.Knowledge](ctx, a, owner, art.KnowledgeID, "knowledge")
	if err != nil {
		return k, err
	}
	titles := []string{k.Title}
	seen := map[string]bool{art.KnowledgeID: true}
	for _, id := range art.RelatedKnowledge {
		if seen[id] {
			continue
		}
		seen[id] = true
		_, other, err := get[domain.Knowledge](ctx, a, owner, id, "knowledge")
		if err != nil {
			return k, err
		}
		if len(art.GroupedLessons) > 0 {
			titles = append(titles, other.Title)
			k.Terms = append(k.Terms, other.Terms...)
			k.Images = append(k.Images, other.Images...)
		}
		k.Units = append(k.Units, other.Units...)
		k.Claims = append(k.Claims, other.Claims...)
		k.Issues = append(k.Issues, other.Issues...)
	}
	if len(art.GroupedLessons) > 0 {
		k.Title = strings.Join(titles, " / ")
	}
	return k, nil
}
