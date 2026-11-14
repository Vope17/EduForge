package app

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"eduforge/internal/domain"
	"eduforge/internal/research"
)

// SuggestEdits asks a model to apply an instruction to selected blocks of an
// output. Nothing is saved: the editor shows the proposals, the user accepts
// some and saves a new version as usual.
func (a *App) SuggestEdits(ctx context.Context, owner, artifactID, instruction string, blocks []research.EditBlock) ([]research.EditResult, error) {
	r, art, err := get[domain.Artifact](ctx, a, owner, artifactID, "artifact")
	if err != nil {
		return nil, err
	}
	instruction = strings.TrimSpace(instruction)
	if instruction == "" || utf8.RuneCountInString(instruction) > 2000 {
		return nil, errors.New("edit_instruction_required")
	}
	if len(blocks) == 0 || len(blocks) > 80 {
		return nil, errors.New("edit_selection_required")
	}
	total, ids := 0, map[string]bool{}
	for _, b := range blocks {
		if b.ID == "" || ids[b.ID] || !research.ValidEditKind(b.Kind) || b.MaxChars < 0 {
			return nil, errors.New("invalid_edit_block")
		}
		ids[b.ID] = true
		total += len(b.Text) + len(b.Context)
	}
	if total > 120000 {
		return nil, errors.New("edit_selection_too_large")
	}
	k, err := a.ArtifactKnowledge(ctx, owner, art)
	if err != nil {
		return nil, err
	}
	refs, err := a.ProviderRefs(ctx, owner)
	if err != nil {
		return nil, err
	}
	evidence, cited := research.EditEvidence(k, blocks, 24000)
	prompt := research.EditPrompt(k, art.Type, art.Language, instruction, blocks, evidence)
	var edits map[string]string
	_, provider, err := a.Providers.Complete(ctx, owner, refs, prompt, func(output string) error {
		parsed, err := research.ParseEdits(output, blocks)
		if err == nil {
			edits = parsed
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	out := []research.EditResult{}
	for _, b := range blocks {
		if text, ok := edits[b.ID]; ok {
			out = append(out, research.CheckEdit(b, text, instruction, cited[b.ID]))
		}
	}
	a.Store.Event(ctx, owner, r.Course, r.Lesson, "ai_edit_suggested", map[string]any{"artifact": artifactID, "blocks": len(blocks), "edits": len(out), "provider": provider})
	return out, nil
}
