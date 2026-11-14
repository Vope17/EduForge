package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

func TestTextCannotAuthorizeAnotherUsersFile(t *testing.T) {
	a, alice, course, _ := fixture(t)
	ctx := context.Background()
	bob, err := a.Store.AddUser(ctx, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	file, sum, err := a.Store.Blob([]byte("private-source-content"), ".png")
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.create(ctx, bob, "source", "", "", "", domain.Source{Role: "design", Path: file, SHA256: sum})
	if err != nil {
		t.Fatal(err)
	}
	_, err = set[domain.Course](ctx, a, alice, course.ID, 0, func(p *domain.Course) error {
		p.Description = file
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := a.Export(ctx, alice, course.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(exported), int64(len(exported)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range z.File {
		if entry.Name == file {
			t.Fatal("arbitrary course description authorized a private blob")
		}
	}
}

func fixture(t *testing.T) (*App, string, domain.Record, domain.Record) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	s, err := store.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	a, err := New(s, filepath.Join(root, "secrets"))
	if err != nil {
		t.Fatal(err)
	}
	owner := s.LocalUser
	course, err := a.CreateCourse(ctx, owner, domain.Course{Name: "Acceptance fixture"})
	if err != nil {
		t.Fatal(err)
	}
	lesson, err := a.CreateLesson(ctx, owner, course.ID, domain.Lesson{Title: "Synthetic nearest-centroid study"})
	if err != nil {
		t.Fatal(err)
	}
	text := "Motivation: classification. Method: nearest centroid. Dataset: synthetic-v1. Results: accuracy is 1.00. Limitation: synthetic data only."
	unit := domain.Unit{ID: domain.ID() + ":p1", Kind: "page", Locator: 1, Text: text}
	claims := []domain.Claim{}
	for i := 0; i < 6; i++ {
		claims = append(claims, domain.Claim{Section: i, Text: text, Citations: []string{unit.ID}, Verified: true, Kind: "source_excerpt"})
	}
	k, err := a.create(ctx, owner, "knowledge", lesson.ID, course.ID, lesson.ID, domain.Knowledge{Title: "Synthetic nearest-centroid study", Units: []domain.Unit{unit}, Claims: claims, Confirmed: true, Terms: []domain.Term{}, Images: []domain.Image{}, Issues: []domain.Issue{}})
	if err != nil {
		t.Fatal(err)
	}
	lesson, err = set[domain.Lesson](ctx, a, owner, lesson.ID, 0, func(p *domain.Lesson) error {
		p.KnowledgeID = k.ID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	providers, err := s.List(ctx, owner, store.Filter{Kind: "provider"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range providers {
		_, err = set[domain.Provider](ctx, a, owner, r.ID, 0, func(p *domain.Provider) error {
			p.Enabled = p.Kind == "source-extract"
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return a, owner, course, lesson
}

func TestSelectedBranchLanguageAndTaskCheckpoint(t *testing.T) {
	ctx := context.Background()
	a, owner, course, lesson := fixture(t)
	req, err := a.CreateRequest(ctx, owner, course.ID, domain.Request{LessonIDs: []string{lesson.ID}, Selected: []string{"A"}, Languages: map[string]string{"A": "en"}, UILanguage: "zh-TW", Unlimited: true})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := a.StartRequest(ctx, owner, req.ID, "zh-TW", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatal("unselected branch started")
	}
	if err = a.process(ctx, owner, tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	rows, err := a.Store.List(ctx, owner, store.Filter{Kind: "artifact", Lesson: lesson.ID})
	if err != nil || len(rows) != 1 {
		t.Fatal("artifact not created")
	}
	art, _ := domain.Decode[domain.Artifact](rows[0].Data)
	if art.Type != "A" || art.Language != "en" || art.Check.Status != "draft" {
		t.Fatalf("wrong output: %#v", art)
	}
	steps, _ := a.Store.List(ctx, owner, store.Filter{Kind: "step", Parent: tasks[0].ID})
	if len(steps) != 7 {
		t.Fatal("subtask checkpoints missing")
	}
	if err = a.process(ctx, owner, tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	after, _ := a.Store.List(ctx, owner, store.Filter{Kind: "step", Parent: tasks[0].ID})
	if len(after) != len(steps) {
		t.Fatal("completed work was repeated")
	}
	if _, err = a.StartRequest(ctx, owner, req.ID, "en", true); err == nil {
		t.Fatal("duplicate branch dispatch accepted")
	}
}
func TestAllProvidersUnavailablePausesWithoutDiscardingSteps(t *testing.T) {
	ctx := context.Background()
	a, owner, course, lesson := fixture(t)
	req, err := a.CreateRequest(ctx, owner, course.ID, domain.Request{LessonIDs: []string{lesson.ID}, Selected: []string{"A"}, Languages: map[string]string{}, UILanguage: "en", Unlimited: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = set[domain.Request](ctx, a, owner, req.ID, 0, func(v *domain.Request) error {
		v.Providers = nil
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := a.StartRequest(ctx, owner, req.ID, "en", true)
	if err != nil {
		t.Fatal(err)
	}
	err = a.process(ctx, owner, tasks[0].ID)
	if !errors.Is(err, domain.ErrPaused) {
		t.Fatalf("should pause: %v", err)
	}
	artifacts, _ := a.Store.List(ctx, owner, store.Filter{Kind: "artifact"})
	if len(artifacts) != 0 {
		t.Fatal("fabricated output without providers")
	}
}
func TestKnowledgeRevisionRejectsStaleSave(t *testing.T) {
	ctx := context.Background()
	a, owner, _, lesson := fixture(t)
	p, _ := domain.Decode[domain.Lesson](lesson.Data)
	old, k, err := get[domain.Knowledge](ctx, a, owner, p.KnowledgeID, "knowledge")
	if err != nil {
		t.Fatal(err)
	}
	k.Title = "Reviewed title"
	revision, err := a.SaveKnowledge(ctx, owner, lesson.ID, old.Version, k)
	if err != nil {
		t.Fatal(err)
	}
	if revision.Version != old.Version+1 {
		t.Fatal("revision version did not increment")
	}
	if _, err = a.SaveKnowledge(ctx, owner, lesson.ID, old.Version, k); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale knowledge accepted: %v", err)
	}
}
func TestExportHasNoCredentialsAndImportRemapsOwnership(t *testing.T) {
	ctx := context.Background()
	a, owner, course, _ := fixture(t)
	providers, _ := a.Store.List(ctx, owner, store.Filter{Kind: "provider"})
	if err := a.Providers.SetSecret(owner, providers[0].ID, "DO-NOT-EXPORT-THIS-KEY"); err != nil {
		t.Fatal(err)
	}
	b, err := a.Export(ctx, owner, course.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range archive.File {
		r, _ := f.Open()
		data, _ := io.ReadAll(r)
		_ = r.Close()
		if bytes.Contains(data, []byte("DO-NOT-EXPORT-THIS-KEY")) {
			t.Fatal("credential leaked")
		}
	}
	bob, err := a.Store.AddUser(ctx, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	imported, err := a.Import(ctx, bob, b, false)
	if err != nil {
		t.Fatal(err)
	}
	if imported.ID == course.ID || imported.Owner != bob {
		t.Fatal("ownership or identifiers not remapped")
	}
	if _, err = a.Store.Get(ctx, owner, imported.ID, "course"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("imported course visible to original owner")
	}
	var p domain.Course
	if json.Unmarshal(imported.Data, &p) != nil || p.Name != "Acceptance fixture" {
		t.Fatal("course data lost")
	}
}

func TestKnowledgeIssuesCanOnlyBeResolvedNotRewritten(t *testing.T) {
	ctx := context.Background()
	a, owner, _, lesson := fixture(t)
	p, _ := domain.Decode[domain.Lesson](lesson.Data)
	parsed := []domain.Issue{{Code: "formula_review", Citation: "u:p1"}, {Code: "formula_review", Citation: "u:p2"}}
	if _, err := set[domain.Knowledge](ctx, a, owner, p.KnowledgeID, 0, func(k *domain.Knowledge) error { k.Issues = parsed; return nil }); err != nil {
		t.Fatal(err)
	}
	old, k, err := get[domain.Knowledge](ctx, a, owner, p.KnowledgeID, "knowledge")
	if err != nil {
		t.Fatal(err)
	}
	k.Issues = []domain.Issue{
		{Code: "formula_review", Citation: "u:p1", Resolved: true},
		{Code: "something_else", Citation: "u:p2", Resolved: true}, // does not match the stored item
		{Code: "injected", Resolved: false},
	}
	revision, err := a.SaveKnowledge(ctx, owner, lesson.ID, old.Version, k)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := domain.Decode[domain.Knowledge](revision.Data)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Issues) != 2 || saved.Issues[0].Code != "formula_review" || saved.Issues[1].Code != "formula_review" {
		t.Fatalf("issue list was rewritten: %#v", saved.Issues)
	}
	if !saved.Issues[0].Resolved || saved.Issues[1].Resolved {
		t.Fatalf("resolution not applied only to the matching item: %#v", saved.Issues)
	}
}
