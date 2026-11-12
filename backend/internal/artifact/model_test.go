package artifact

import "testing"

func TestNestedSlideBlockIDsRemainStable(t *testing.T) {
	content := Content{
		Title: "Water cycle",
		Blocks: []Block{{
			Kind: "slide", Layout: "title_body", Notes: "Explain evaporation.",
			Children: []Block{{Kind: "heading", Text: "Water cycle"}, {Kind: "paragraph", Text: "Evaporation"}},
		}},
	}
	if err := content.ValidateFor(SlidesType); err != nil {
		t.Fatal(err)
	}
	if err := content.AssignIDs(nil); err != nil {
		t.Fatal(err)
	}
	pageID := content.Blocks[0].ID
	textID := content.Blocks[0].Children[1].ID
	original := FlattenBlocks(content.Blocks)
	seen := make(map[string]bool)
	for _, block := range original {
		if block.ID == "" || seen[block.ID] {
			t.Fatal("missing or duplicate nested ID")
		}
		seen[block.ID] = true
	}
	previous := append([]Block(nil), content.Blocks...)
	content.Blocks[0].Children[1].Text = "Updated explanation"
	if err := content.AssignIDs(previous); err != nil {
		t.Fatal(err)
	}
	if content.Blocks[0].ID != pageID || content.Blocks[0].Children[1].ID != textID {
		t.Fatal("editing changed a stable block ID")
	}
	content.Blocks[0].Children = append(content.Blocks[0].Children, content.Blocks[0].Children[1])
	if err := content.AssignIDs(previous); err == nil {
		t.Fatal("accepted a reused nested ID")
	}
}

func TestDocumentsRejectSlideOnlyFields(t *testing.T) {
	for _, block := range []Block{
		{Kind: "slide", Layout: "title_body", Children: []Block{{Kind: "heading"}, {Kind: "paragraph"}}},
		{Kind: "paragraph", Text: "Text", Notes: "Slide notes"},
		{Kind: "paragraph", Text: "Text", Children: []Block{{Kind: "heading", Text: "Hidden"}}},
	} {
		if err := (Content{Title: "Document", Blocks: []Block{block}}).Validate(); err == nil {
			t.Fatal("accepted slide content as a document")
		}
	}
}
