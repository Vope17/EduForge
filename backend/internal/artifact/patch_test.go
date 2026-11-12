package artifact

import (
	"errors"
	"reflect"
	"testing"
)

func testDeck() Content {
	return Content{Title: "Lesson", Blocks: []Block{{ID: "page", Kind: "slide", Layout: "two_columns", Notes: "private speaker notes", Children: []Block{
		{ID: "heading", Kind: "heading", Text: "Water"},
		{ID: "body", Kind: "paragraph", Text: "A😀BC水D"},
		{ID: "list", Kind: "list", Text: "Steps", Items: []string{"Evaporate", "Condense"}},
	}}}}
}

func ptr(value int) *int { return &value }

func TestSelectedRangesUseUTF16AndPreserveUnselectedContent(t *testing.T) {
	original := testDeck()
	snapshot := original.Clone()
	anchors := []Anchor{
		{ArtifactID: "deck", BlockID: "body", Start: ptr(1), End: ptr(3)},
		{ArtifactID: "deck", BlockID: "body", Start: ptr(5), End: ptr(6)},
		{ArtifactID: "deck", BlockID: "list", Field: "items.1"},
	}
	result, err := ApplyEdits("deck", SlidesType, original, anchors, []Edit{
		{BlockID: "body", Field: "text", Start: 1, End: 3, Replacement: "🌧️ rain"},
		{BlockID: "body", Field: "text", Start: 5, End: 6, Replacement: "water"},
		{BlockID: "list", Field: "items.1", Start: 0, End: 8, Replacement: "Clouds form"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Blocks[0].Children[1].Text != "A🌧️ rainBCwaterD" || result.Blocks[0].Children[2].Items[0] != "Evaporate" ||
		result.Blocks[0].Children[2].Items[1] != "Clouds form" || result.Blocks[0].Notes != snapshot.Blocks[0].Notes {
		t.Fatalf("unexpected patch result: %+v", result)
	}
	if !reflect.DeepEqual(original, snapshot) {
		t.Fatal("patch mutated its input")
	}
	for i, block := range FlattenBlocks(result.Blocks) {
		if block.ID != FlattenBlocks(original.Blocks)[i].ID {
			t.Fatal("patch changed an ID")
		}
	}
}

func TestOutOfScopePatchRejected(t *testing.T) {
	selected := []Anchor{{ArtifactID: "deck", BlockID: "body", Start: ptr(1), End: ptr(3)}}
	for _, edit := range []Edit{
		{BlockID: "heading", Field: "text", Start: 0, End: 5, Replacement: "Unselected"},
		{BlockID: "page", Field: "notes", Start: 0, End: 21, Replacement: "Unselected"},
		{BlockID: "body", Field: "text", Start: 0, End: 3, Replacement: "Outside range"},
		{BlockID: "body", Field: "image_url", Start: 1, End: 3, Replacement: "https://example.com"},
	} {
		if _, err := ApplyEdits("deck", SlidesType, testDeck(), selected, []Edit{edit}); !errors.Is(err, ErrSelection) {
			t.Fatalf("out-of-scope edit accepted: %+v, %v", edit, err)
		}
	}
	for _, anchor := range []Anchor{
		{ArtifactID: "other", BlockID: "body"},
		{ArtifactID: "deck", BlockID: "missing"},
		{ArtifactID: "deck", BlockID: "body", Start: ptr(2), End: ptr(3)},
		{ArtifactID: "deck", BlockID: "body", Start: ptr(0), End: ptr(99)},
		{ArtifactID: "deck", BlockID: "body", Field: "notes"},
	} {
		if _, err := ResolveSelection("deck", testDeck(), []Anchor{anchor}); !errors.Is(err, ErrSelection) {
			t.Fatalf("invalid anchor accepted: %+v, %v", anchor, err)
		}
	}
}

func TestOverlappingSelectionsMergeAndListSelectionIncludesItems(t *testing.T) {
	segments, err := ResolveSelection("deck", testDeck(), []Anchor{
		{ArtifactID: "deck", BlockID: "body", Start: ptr(0), End: ptr(4)},
		{ArtifactID: "deck", BlockID: "body", Start: ptr(3), End: ptr(5)},
		{ArtifactID: "deck", BlockID: "list"},
	})
	if err != nil || len(segments) != 4 || segments[0].Text != "A😀BC" || segments[0].End != 5 {
		t.Fatalf("segments %+v: %v", segments, err)
	}
}
