package generate

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"eduforge/backend/internal/artifact"
	"eduforge/backend/internal/llm"
	"eduforge/backend/internal/store"
	"eduforge/backend/internal/template"
)

func slidePage(layout string) artifact.Block {
	return artifact.Block{Kind: "slide", Layout: layout, Text: "", Notes: "Ask the class to explain.", Children: []artifact.Block{
		{Kind: "heading", Text: "Water"}, {Kind: "paragraph", Text: "Evaporation and condensation"},
	}}
}

func slideProvider(language artifact.Language, layout string, count int) providerFunc {
	return func(_ context.Context, request llm.Request) (json.RawMessage, error) {
		var input struct {
			Template *template.Spec `json:"confirmed_template"`
		}
		if err := json.Unmarshal(request.Data, &input); err != nil {
			return nil, err
		}
		if input.Template == nil {
			return json.Marshal(map[string]any{"language": language, "palette": "ocean", "font": "sans", "layout": layout, "tone": "friendly", "density": "balanced", "example": []artifact.Block{slidePage(layout)}})
		}
		pages := make([]artifact.Block, count)
		for i := range pages {
			pages[i] = slidePage(layout)
		}
		return json.Marshal(map[string]any{"language": language, "title": "Water", "blocks": pages})
	}
}

func TestSlideGateLanguageLayoutAndImmutableNestedBlocks(t *testing.T) {
	for _, language := range []artifact.Language{artifact.TraditionalChinese, artifact.English} {
		for _, layout := range []string{"title_body", "two_columns", "image_text"} {
			t.Run(string(language)+"/"+layout, func(t *testing.T) {
				db := database(t)
				ctx := t.Context()
				fullCalls := 0
				mock := slideProvider(language, layout, 2)
				service := New(db, providerFunc(func(ctx context.Context, r llm.Request) (json.RawMessage, error) {
					if strings.Contains(string(r.Data), `"confirmed_template"`) {
						fullCalls++
					}
					var input map[string]any
					if err := json.Unmarshal(r.Data, &input); err != nil {
						t.Fatal(err)
					}
					if input["language"] != string(language) || input["slide_count"] != float64(2) {
						t.Fatalf("wrong slide request: %s", r.Data)
					}
					return mock(ctx, r)
				}), 20)
				request, err := service.Create(ctx, "teacher", Input{Type: artifact.SlidesType, Kind: "presentation", Topic: "Water", Language: language, SlideCount: 2})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := service.Generate(ctx, "teacher", request.ID, 0); !errors.Is(err, store.ErrConflict) {
					t.Fatalf("ungated: %v", err)
				}
				preview, err := service.Preview(ctx, "teacher", request.ID, 0, "")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := service.Generate(ctx, "teacher", request.ID, preview.Version); !errors.Is(err, store.ErrConflict) {
					t.Fatalf("preview bypass: %v", err)
				}
				if fullCalls != 0 {
					t.Fatal("full generation called before confirmation")
				}
				if _, err := service.Confirm(ctx, "teacher", request.ID, preview.Version); err != nil {
					t.Fatal(err)
				}
				deck, err := service.Generate(ctx, "teacher", request.ID, preview.Version)
				if err != nil {
					t.Fatal(err)
				}
				if deck.Type != artifact.SlidesType || deck.Language != language || len(deck.Content.Blocks) != 2 || fullCalls != 1 {
					t.Fatalf("wrong deck %+v", deck)
				}
				ids := map[string]bool{}
				for _, block := range artifact.FlattenBlocks(deck.Content.Blocks) {
					if block.ID == "" || ids[block.ID] {
						t.Fatal("missing/reused nested ID")
					}
					ids[block.ID] = true
				}
				changed := deck.Content.Clone()
				changed.Blocks[0], changed.Blocks[1] = changed.Blocks[1], changed.Blocks[0]
				changed.Blocks[0].Notes = "Updated notes"
				saved, err := db.ChangeDocument(ctx, "teacher", deck.ID, store.Change{BaseRevision: 1, Action: "save", Content: &changed})
				if err != nil || saved.Content.Blocks[0].ID != deck.Content.Blocks[1].ID {
					t.Fatalf("nested save: %+v %v", saved, err)
				}
				old, err := db.Document(ctx, "teacher", deck.ID, 1)
				if err != nil || !reflect.DeepEqual(old.Content, deck.Content) {
					t.Fatal("old revision changed", err)
				}
				undo, err := db.ChangeDocument(ctx, "teacher", deck.ID, store.Change{BaseRevision: 2, Action: "undo"})
				if err != nil || !reflect.DeepEqual(undo.Content, deck.Content) {
					t.Fatal("slide undo failed", err)
				}
				redo, err := db.ChangeDocument(ctx, "teacher", deck.ID, store.Change{BaseRevision: 3, Action: "redo"})
				if err != nil || !reflect.DeepEqual(redo.Content, saved.Content) {
					t.Fatal("slide redo failed", err)
				}
			})
		}
	}
}

func TestInvalidSlideGenerationNeverCreatesRevision(t *testing.T) {
	for _, mode := range []string{"count", "layout", "ids", "language"} {
		t.Run(mode, func(t *testing.T) {
			db := database(t)
			ctx := t.Context()
			mock := slideProvider(artifact.English, "title_body", 1)
			service := New(db, providerFunc(func(ctx context.Context, r llm.Request) (json.RawMessage, error) {
				data, err := mock(ctx, r)
				if err != nil {
					return nil, err
				}
				if !strings.Contains(string(r.Data), `"confirmed_template"`) {
					return data, nil
				}
				page := slidePage("title_body")
				language := artifact.English
				pages := []artifact.Block{page}
				switch mode {
				case "count":
					pages = append(pages, page)
				case "layout":
					pages[0].Layout = "two_columns"
				case "ids":
					pages[0].Children[0].ID = "invented"
				case "language":
					language = artifact.TraditionalChinese
				}
				return json.Marshal(map[string]any{"language": language, "title": "Water", "blocks": pages})
			}), 20)
			request, err := service.Create(ctx, "teacher", Input{Type: artifact.SlidesType, Kind: "presentation", Topic: "Water", Language: artifact.English, SlideCount: 1})
			if err != nil {
				t.Fatal(err)
			}
			preview, err := service.Preview(ctx, "teacher", request.ID, 0, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Confirm(ctx, "teacher", request.ID, preview.Version); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Generate(ctx, "teacher", request.ID, preview.Version); !errors.Is(err, llm.ErrInvalidOutput) {
				t.Fatalf("invalid %s accepted: %v", mode, err)
			}
			if _, err := db.Document(ctx, "teacher", request.ID, 0); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("invalid deck persisted", err)
			}
			current, err := db.Request(ctx, "teacher", request.ID)
			if err != nil || current.State != template.Confirmed {
				t.Fatal("cannot retry invalid generation", err)
			}
		})
	}
}
