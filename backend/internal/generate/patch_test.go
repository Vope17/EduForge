package generate

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"eduforge/backend/internal/artifact"
	"eduforge/backend/internal/llm"
	"eduforge/backend/internal/store"
)

func TestSlideProposalIsScopedReviewedAndRevisionBound(t *testing.T) {
	db := database(t)
	ctx := t.Context()
	service := New(db, slideProvider(artifact.English, "title_body", 1), 30)
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
	deck, err := service.Generate(ctx, "teacher", request.ID, preview.Version)
	if err != nil {
		t.Fatal(err)
	}
	target := deck.Content.Blocks[0].Children[1]
	anchors := []artifact.Anchor{{ArtifactID: deck.ID, BlockID: target.ID}}
	unselected := deck.Content.Blocks[0].Children[0].ID
	bad := false
	expectedText := target.Text
	service.provider = providerFunc(func(_ context.Context, r llm.Request) (json.RawMessage, error) {
		if strings.Contains(string(r.Data), deck.Content.Blocks[0].Notes) || strings.Contains(string(r.Data), unselected) {
			t.Fatal("unselected content sent to provider")
		}
		var input struct {
			Selection []artifact.Segment `json:"selected_text"`
		}
		if err := json.Unmarshal(r.Data, &input); err != nil {
			t.Fatal(err)
		}
		if len(input.Selection) != 1 || input.Selection[0].Text != expectedText {
			t.Fatalf("wrong provider scope: %s", r.Data)
		}
		segment := input.Selection[0]
		id := segment.BlockID
		if bad {
			id = unselected
		}
		return json.Marshal(map[string]any{"edits": []artifact.Edit{{BlockID: id, Field: segment.Field, Start: segment.Start, End: segment.End, Replacement: "Water changes state."}}})
	})
	bad = true
	if _, err := service.Propose(ctx, "teacher", deck.ID, 1, "Simplify", anchors); !errors.Is(err, artifact.ErrSelection) {
		t.Fatalf("unselected patch accepted: %v", err)
	}
	bad = false
	proposal, err := service.Propose(ctx, "teacher", deck.ID, 1, "Simplify", anchors)
	if err != nil {
		t.Fatal(err)
	}
	current, err := db.Document(ctx, "teacher", deck.ID, 0)
	if err != nil || current.Revision != 1 || current.Content.Blocks[0].Children[1].Text != target.Text {
		t.Fatal("proposal was applied before acceptance", err)
	}
	if proposal.BaseRevision != 1 || len(proposal.Changes) != 1 || proposal.Changes[0].Before != target.Text {
		t.Fatalf("missing diff %+v", proposal)
	}
	accepted, err := db.AcceptProposal(ctx, "teacher", deck.ID, proposal.ID)
	if err != nil || accepted.Revision != 2 || accepted.Content.Blocks[0].Children[1].Text != "Water changes state." {
		t.Fatalf("accept %+v %v", accepted, err)
	}
	if accepted.Content.Blocks[0].Notes != deck.Content.Blocks[0].Notes || accepted.Content.Blocks[0].Children[0].ID != unselected {
		t.Fatal("unselected block changed")
	}
	duplicate, err := db.AcceptProposal(ctx, "teacher", deck.ID, proposal.ID)
	if err != nil || duplicate.Revision != 2 {
		t.Fatal("accept retry created duplicate revision", err)
	}
	old, err := db.Document(ctx, "teacher", deck.ID, 1)
	if err != nil || old.Content.Blocks[0].Children[1].Text != target.Text {
		t.Fatal("old revision mutated", err)
	}

	expectedText = "Water changes state."
	stale, err := service.Propose(ctx, "teacher", deck.ID, 2, "Simplify", anchors)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ChangeDocument(ctx, "teacher", deck.ID, store.Change{BaseRevision: 2, Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AcceptProposal(ctx, "teacher", deck.ID, stale.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatal("stale proposal accepted", err)
	}
	if err := db.RejectProposal(ctx, "teacher", deck.ID, stale.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AcceptProposal(ctx, "teacher", deck.ID, stale.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatal("rejected proposal accepted", err)
	}
	if _, err := db.AcceptProposal(ctx, "other", deck.ID, proposal.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("cross-user proposal accepted", err)
	}
}
