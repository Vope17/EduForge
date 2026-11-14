package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestTemplateAIEditsOnlySelectedValidSettings(t *testing.T) {
	ctx := context.Background()
	a, owner, _, _ := fixture(t)
	b, _ := os.ReadFile("../../tests/fixtures/template.potx")
	r, err := a.ImportTemplate(ctx, owner, "Lab", "template.potx", b)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := a.TemplateSpec(ctx, owner, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	prompts := useFakeModel(t, a, owner, func(string) string {
		// accent1 is valid; title_box runs off the slide; dk1 was not selected.
		return `{"changes":{"colors.accent1":"#1f4e79","title_box":[0.5,0.1,0.9,0.2],"colors.dk1":"000000"}}`
	})
	changes, err := a.SuggestTemplateEdits(ctx, owner, r.ID, "沉穩的藍色", spec, []string{"colors.accent1", "title_box"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Key != "colors.accent1" || string(changes[0].After) != `"1F4E79"` {
		t.Fatalf("only the valid, selected change should remain: %+v", changes)
	}
	last := (*prompts)[len(*prompts)-1]
	if !strings.Contains(last, "colors.accent1 = ") || !strings.Contains(last, "← selected") || !strings.Contains(last, "沉穩的藍色") {
		t.Fatal("the prompt should show the design and mark the selection")
	}
	if _, err = a.SuggestTemplateEdits(ctx, owner, r.ID, "x", spec, []string{"styles.h1"}); err == nil {
		t.Fatal("a Word key on a slide template should be rejected")
	}
}

func TestWordTemplateAIEditsMergeStyleFields(t *testing.T) {
	ctx := context.Background()
	a, owner, _, _ := fixture(t)
	b, _ := os.ReadFile("../../tests/fixtures/reader-template.dotx")
	r, err := a.ImportTemplate(ctx, owner, "Reader", "reader-template.dotx", b)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := a.TemplateSpec(ctx, owner, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	useFakeModel(t, a, owner, func(string) string {
		return `{"changes":{"styles.h1":{"size":20,"color":"1F4E79"},"margins":{"left":30},"footer":"頁尾文字"}}`
	})
	changes, err := a.SuggestTemplateEdits(ctx, owner, r.ID, "標題更醒目", spec, []string{"styles.h1", "margins", "footer"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 {
		t.Fatalf("want 3 changes, got %+v", changes)
	}
	var h1 struct {
		Size  float64 `json:"size"`
		Color string  `json:"color"`
		Font  string  `json:"font"`
	}
	_ = json.Unmarshal(changes[2].After, &h1) // sorted: footer, margins, styles.h1
	if changes[2].Key != "styles.h1" || h1.Size != 20 || h1.Color != "1F4E79" || h1.Font != spec.Styles["h1"].Font {
		t.Fatalf("style fields should merge into the current style: %s %+v", changes[2].Key, h1)
	}
	var m struct{ Left, Top float64 }
	_ = json.Unmarshal(changes[1].After, &m)
	if m.Left != 30 || m.Top != spec.Margins.Top {
		t.Fatalf("margins should merge: %+v", m)
	}
}
