package app

import (
	"context"
	"strings"
	"testing"

	"eduforge/internal/domain"
	"eduforge/internal/research"
	"eduforge/internal/store"
)

func TestSuggestEditsReturnsCheckedProposalsWithoutSaving(t *testing.T) {
	ctx := context.Background()
	a, owner, course, lesson := fixture(t)
	p, _ := domain.Decode[domain.Lesson](lesson.Data)
	_, k, _ := get[domain.Knowledge](ctx, a, owner, p.KnowledgeID, "knowledge")
	cite := k.Units[0].ID
	art, err := a.create(ctx, owner, "artifact", lesson.ID, course.ID, lesson.ID, domain.Artifact{Type: "B", Language: "zh-TW", KnowledgeID: p.KnowledgeID, Sections: []domain.Section{{Title: "方法", Body: "最近質心分類", Citations: []string{cite}}}})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	prompts := useFakeModel(t, a, owner, func(prompt string) string {
		calls++
		if calls == 1 {
			return "Sure, here is the text." // not JSON: the next attempt must be used
		}
		// 1.00 is in the source; 42 is invented; an unknown id is ignored.
		return "```json\n" + `{"edits":[{"id":"s0:body","text":"準確率 1.00，使用 42 個樣本"},{"id":"s0:title","text":"方法概述"},{"id":"other","text":"x"}]}` + "\n```"
	})
	blocks := []research.EditBlock{
		{ID: "s0:title", Kind: "slide_title", Text: "方法", Citations: []string{cite}},
		{ID: "s0:body", Kind: "slide_text", Text: "最近質心分類", Context: "方法", Citations: []string{cite}, MaxChars: 8},
	}
	if _, err = a.SuggestEdits(ctx, owner, art.ID, "精簡", blocks); err == nil {
		t.Fatal("an invalid reply from the only provider should fail")
	}
	out, err := a.SuggestEdits(ctx, owner, art.ID, "精簡", blocks)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].ID != "s0:title" || out[1].ID != "s0:body" {
		t.Fatalf("edits should follow the block order and drop unknown ids: %+v", out)
	}
	if got := out[1].UnsupportedNumbers; len(got) != 1 || got[0] != "42" {
		t.Fatalf("only the invented number should be flagged, got %v", got)
	}
	if !out[1].Overflow {
		t.Fatal("text far beyond the box limit should be flagged")
	}
	last := (*prompts)[len(*prompts)-1]
	for _, want := range []string{"精簡", "s0:body", "Synthetic nearest-centroid", "Traditional Chinese", "about 8 characters"} {
		if !strings.Contains(last, want) {
			t.Fatalf("prompt should mention %q", want)
		}
	}
	if n := count(t, a, owner, store.Filter{Kind: "artifact", Lesson: lesson.ID}); n != 1 {
		t.Fatalf("suggestions must not create versions, found %d artifacts", n)
	}
}

func TestSuggestEditsValidatesInputAndNeedsAModel(t *testing.T) {
	ctx := context.Background()
	a, owner, course, lesson := fixture(t)
	p, _ := domain.Decode[domain.Lesson](lesson.Data)
	art, err := a.create(ctx, owner, "artifact", lesson.ID, course.ID, lesson.ID, domain.Artifact{Type: "A", Language: "en", KnowledgeID: p.KnowledgeID})
	if err != nil {
		t.Fatal(err)
	}
	ok := []research.EditBlock{{ID: "a", Kind: "body", Text: "x"}}
	for name, c := range map[string]struct {
		instruction string
		blocks      []research.EditBlock
		want        string
	}{
		"empty instruction": {" ", ok, "edit_instruction_required"},
		"no blocks":         {"shorten", nil, "edit_selection_required"},
		"unknown kind":      {"shorten", []research.EditBlock{{ID: "a", Kind: "script", Text: "x"}}, "invalid_edit_block"},
		"duplicate ids":     {"shorten", []research.EditBlock{{ID: "a", Kind: "body"}, {ID: "a", Kind: "title"}}, "invalid_edit_block"},
		// The fixture enables only the offline test mode, which cannot edit.
		"offline only": {"shorten", ok, "model_connection_required"},
	} {
		if _, err := a.SuggestEdits(ctx, owner, art.ID, c.instruction, c.blocks); err == nil || err.Error() != c.want {
			t.Fatalf("%s: want %s, got %v", name, c.want, err)
		}
	}
}
