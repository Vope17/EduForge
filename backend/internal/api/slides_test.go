package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"eduforge/backend/internal/generate"
	"eduforge/backend/internal/job"
	"eduforge/backend/internal/llm"
	"eduforge/backend/internal/store"
)

type previewProvider struct{ calls int }

func (p *previewProvider) Ready() bool { return true }
func (p *previewProvider) Generate(context.Context, llm.Request) (json.RawMessage, error) {
	p.calls++
	return json.RawMessage(`{"language":"en","palette":"forest","font":"sans","layout":"title_body","tone":"friendly","density":"balanced","example":[{"kind":"slide","text":"","layout":"title_body","notes":"Ask why","children":[{"kind":"heading","text":"Water"},{"kind":"paragraph","text":"Water evaporates"}]}]}`), nil
}

func TestSlidesHTTPGateJobAndResourceIsolation(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "api.sqlite"))
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
	provider := &previewProvider{}
	service := generate.New(db, provider, 20)
	handler := NewHandler(NewDocuments(db, service, job.New(db, service)), nil)
	call := func(method, path string, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
		var data []byte
		if body != nil {
			var err error
			data, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, path, bytes.NewReader(data)))
		if response.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, response.Code, response.Body.String())
		}
		return response
	}
	create := func(path, kind string, count int) requestDTO {
		result := call(http.MethodPost, path, map[string]any{"topic": "Water", "audience": "Grade 5", "language": "en", "kind": kind, "slide_count": count}, http.StatusCreated)
		var request requestDTO
		if err := json.Unmarshal(result.Body.Bytes(), &request); err != nil {
			t.Fatal(err)
		}
		return request
	}
	deck := create("/api/v1/slides", "presentation", 2)
	doc := create("/api/v1/documents", "handout", 0)
	for _, test := range []struct{ prefix, id string }{{"/api/v1/documents/", deck.ID}, {"/api/v1/slides/", doc.ID}} {
		for _, route := range []struct{ method, suffix string }{
			{"GET", ""}, {"GET", "/revisions"}, {"GET", "/revisions/1"}, {"POST", "/revisions"}, {"POST", "/preview"}, {"POST", "/confirm"}, {"POST", "/generate"}, {"POST", "/patches"}, {"POST", "/patches/fake/accept"}, {"POST", "/patches/fake/reject"},
		} {
			call(route.method, test.prefix+test.id+route.suffix, map[string]any{}, http.StatusNotFound)
		}
	}
	for _, path := range []string{"/api/v1/documents", "/api/v1/slides"} {
		result := call("GET", path, nil, 200)
		var list []summaryDTO
		if err := json.Unmarshal(result.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || path == "/api/v1/slides" && list[0].ID != deck.ID || path == "/api/v1/documents" && list[0].ID != doc.ID {
			t.Fatalf("mixed artifact list: %s", result.Body.String())
		}
	}
	path := "/api/v1/slides/" + deck.ID
	version := map[string]any{"template_version": 0}
	call("POST", path+"/generate", version, 409)
	call("POST", path+"/preview", map[string]any{"template_version": 0, "feedback": ""}, 200)
	version["template_version"] = 1
	call("POST", path+"/generate", version, 409)
	if provider.calls != 1 {
		t.Fatal("full model call before confirmation")
	}
	call("POST", path+"/confirm", version, 200)
	result := call("POST", path+"/generate", version, http.StatusAccepted)
	var queued jobDTO
	if err := json.Unmarshal(result.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	if queued.ID == "" || queued.State != "queued" || queued.ArtifactID != deck.ID || provider.calls != 1 {
		t.Fatalf("generation was not queued: %s", result.Body.String())
	}
	call("GET", "/api/v1/jobs/"+queued.ID, nil, 200)
	result = call("GET", path, nil, 200)
	var details struct {
		Job *jobDTO `json:"job"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &details); err != nil {
		t.Fatal(err)
	}
	if details.Job == nil || details.Job.ID != queued.ID {
		t.Fatal("reload loses job")
	}
	call("POST", "/api/v1/jobs/"+queued.ID+"/cancel", map[string]any{}, 200)
	call("POST", path+"/generate", version, http.StatusAccepted)
}
