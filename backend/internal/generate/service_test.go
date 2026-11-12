package generate

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"eduforge/backend/internal/artifact"
	"eduforge/backend/internal/llm"
	"eduforge/backend/internal/store"
	"eduforge/backend/internal/template"
)

type providerFunc func(context.Context, llm.Request) (json.RawMessage, error)

func (f providerFunc) Ready() bool { return true }

func (f providerFunc) Generate(ctx context.Context, r llm.Request) (json.RawMessage, error) {
	return f(ctx, r)
}

func database(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestConfirmedVersionGatesGeneration(t *testing.T) {
	for _, test := range []struct {
		language artifact.Language
		title    string
	}{
		{artifact.TraditionalChinese, "水循環學習單"},
		{artifact.English, "The water cycle"},
	} {
		t.Run(string(test.language), func(t *testing.T) {
			db := database(t)
			previewCalls := 0
			fullCalls := 0
			provider := providerFunc(func(_ context.Context, request llm.Request) (json.RawMessage, error) {
				var input struct {
					Language artifact.Language `json:"language"`
					Template *template.Spec    `json:"confirmed_template"`
				}
				if err := json.Unmarshal(request.Data, &input); err != nil {
					t.Fatal(err)
				}
				if input.Language != test.language {
					t.Fatalf("provider received language %q", input.Language)
				}
				if input.Template == nil {
					previewCalls++
					return json.Marshal(map[string]any{
						"language": test.language, "palette": "ocean", "font": "sans",
						"layout": "worksheet", "tone": "friendly", "density": "balanced",
						"example": []artifact.Block{{Kind: "heading", Text: test.title}},
					})
				}
				fullCalls++
				if input.Template.Palette != "ocean" || len(input.Template.Example) != 1 {
					t.Fatal("confirmed template was not passed to full generation")
				}
				return json.Marshal(map[string]any{
					"language": test.language, "title": test.title,
					"blocks": []artifact.Block{{Kind: "paragraph", Text: test.title}},
				})
			})
			service := New(db, provider, 10)
			ctx := t.Context()
			request, err := service.Create(ctx, "teacher", Input{Topic: test.title, Kind: "worksheet", Language: test.language})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Generate(ctx, "teacher", request.ID, 0); !errors.Is(err, store.ErrConflict) {
				t.Fatalf("unconfirmed generate: %v", err)
			}
			first, err := service.Preview(ctx, "teacher", request.ID, 0, "")
			if err != nil || first.State != template.AwaitingConfirmation || first.Version != 1 {
				t.Fatalf("first preview: %+v, %v", first, err)
			}
			if _, err := service.Generate(ctx, "teacher", request.ID, 1); !errors.Is(err, store.ErrConflict) {
				t.Fatalf("unconfirmed preview generated: %v", err)
			}
			second, err := service.Preview(ctx, "teacher", request.ID, 1, "Revise layout")
			if err != nil || second.Version != 2 {
				t.Fatalf("second preview: %+v, %v", second, err)
			}
			if _, err := service.Confirm(ctx, "teacher", request.ID, 1); !errors.Is(err, store.ErrConflict) {
				t.Fatalf("obsolete preview confirmed: %v", err)
			}
			confirmed, err := service.Confirm(ctx, "teacher", request.ID, 2)
			if err != nil || confirmed.ConfirmedVersion != 2 || confirmed.State != template.Confirmed {
				t.Fatalf("confirm: %+v, %v", confirmed, err)
			}
			if _, err := service.Generate(ctx, "teacher", request.ID, 1); !errors.Is(err, store.ErrConflict) {
				t.Fatalf("obsolete version generated: %v", err)
			}
			if fullCalls != 0 {
				t.Fatal("provider called before valid generation")
			}
			document, err := service.Generate(ctx, "teacher", request.ID, 2)
			if err != nil {
				t.Fatal(err)
			}
			if document.Language != test.language || document.Content.Title != test.title || document.Revision != 1 ||
				document.Content.Blocks[0].ID == "" || fullCalls != 1 || previewCalls != 2 {
				t.Fatalf("generated document: %+v, preview calls %d, full calls %d", document, previewCalls, fullCalls)
			}
			if _, err := service.Generate(ctx, "teacher", request.ID, 2); !errors.Is(err, store.ErrConflict) {
				t.Fatalf("duplicate generation: %v", err)
			}
			verifyRevisions(t, db, document)
		})
	}
}

func verifyRevisions(t *testing.T, db *store.Store, original artifact.Artifact) {
	t.Helper()
	ctx := t.Context()
	content := artifact.Content{Title: "Edited", Blocks: append([]artifact.Block(nil), original.Content.Blocks...)}
	content.Blocks[0].Text = "Edited"
	content.Blocks = append(content.Blocks, artifact.Block{Kind: "paragraph", Text: "New"})
	changed, err := db.ChangeDocument(ctx, "teacher", original.ID, store.Change{
		BaseRevision: 1, Action: "save", Content: &content,
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Content.Blocks[0].ID != original.Content.Blocks[0].ID || changed.Content.Blocks[1].ID == "" {
		t.Fatalf("block identity not preserved: %+v", changed.Content.Blocks)
	}
	stale := store.Change{BaseRevision: 1, Action: "save", Content: &content}
	if _, err := db.ChangeDocument(ctx, "teacher", original.ID, stale); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
	undo, err := db.ChangeDocument(ctx, "teacher", original.ID, store.Change{BaseRevision: 2, Action: "undo"})
	if err != nil || undo.Content.Title != original.Content.Title || !undo.CanRedo || undo.Revision != 3 {
		t.Fatalf("undo: %+v, %v", undo, err)
	}
	redo, err := db.ChangeDocument(ctx, "teacher", original.ID, store.Change{BaseRevision: 3, Action: "redo"})
	if err != nil || redo.Content.Title != "Edited" || redo.Revision != 4 {
		t.Fatalf("redo: %+v, %v", redo, err)
	}
	restored, err := db.ChangeDocument(ctx, "teacher", original.ID, store.Change{
		BaseRevision: 4, Action: "restore", TargetRevision: 1,
	})
	if err != nil || restored.Content.Title != original.Content.Title || restored.Revision != 5 {
		t.Fatalf("restore: %+v, %v", restored, err)
	}
	old, err := db.Document(ctx, "teacher", original.ID, 1)
	if err != nil || old.Content.Blocks[0].Text != original.Content.Blocks[0].Text || len(old.Content.Blocks) != 1 {
		t.Fatalf("old revision changed: %+v, %v", old, err)
	}
	if _, err := db.Document(ctx, "other", original.ID, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-user lookup: %v", err)
	}
}

func TestInvalidPreviewReleasesClaimAndCountsAgainstQuota(t *testing.T) {
	db := database(t)
	ctx := t.Context()
	calls := 0
	service := New(db, providerFunc(func(_ context.Context, r llm.Request) (json.RawMessage, error) {
		calls++
		if !strings.Contains(r.Instruction, "style preview only") {
			t.Fatal("wrong instruction")
		}
		return json.RawMessage(`{"language":"en","extra":"unexpected"}`), nil
	}), 1)
	request, err := service.Create(ctx, "teacher", Input{Topic: "天氣", Kind: "handout", Language: artifact.TraditionalChinese})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Preview(ctx, "teacher", request.ID, 0, ""); !errors.Is(err, llm.ErrInvalidOutput) {
		t.Fatalf("invalid output: %v", err)
	}
	if _, err := service.Preview(ctx, "teacher", request.ID, 0, ""); !errors.Is(err, store.ErrLimit) {
		t.Fatalf("expected quota failure after releasing preview: %v", err)
	}
	if calls != 1 {
		t.Fatalf("provider called %d times", calls)
	}
}

func TestSubscriptionErrorsRemainActionable(t *testing.T) {
	for _, expected := range []error{llm.ErrPlanPermission, llm.ErrSubscriptionLimit} {
		service := New(nil, providerFunc(func(context.Context, llm.Request) (json.RawMessage, error) {
			return nil, expected
		}), 30)
		err := service.generate(t.Context(), time.Second, "Return JSON", nil, json.RawMessage(`{}`), 128, new(any))
		if !errors.Is(err, expected) {
			t.Fatalf("subscription error was hidden: %v", err)
		}
	}
}
