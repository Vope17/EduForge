// Package httpapi is the single-user transport boundary; ownership is resolved server-side.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"eduforge/internal/app"
	"eduforge/internal/artifacts"
	"eduforge/internal/domain"
	"eduforge/internal/mathtex"
	"eduforge/internal/research"
	"eduforge/internal/store"
)

type ownerKey struct{}
type Server struct {
	App     *app.App
	Mux     *http.ServeMux
	Token   string
	Web     string
	Origins map[string]bool
	Hosts   map[string]bool
}

func New(a *app.App, web string, origins []string) *Server {
	s := &Server{App: a, Mux: http.NewServeMux(), Token: domain.ID() + domain.ID(), Web: web, Origins: map[string]bool{}, Hosts: map[string]bool{}}
	for _, origin := range origins {
		u, err := url.Parse(origin)
		if err == nil && u.Host != "" {
			s.Origins[origin] = true
			s.Hosts[u.Host] = true
		}
	}
	s.routes()
	return s
}
func owner(r *http.Request) string {
	id, _ := r.Context().Value(ownerKey{}).(string)
	return id
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	if !s.Hosts[r.Host] {
		writeError(w, errors.New("invalid_host"), http.StatusForbidden)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !s.Origins[origin] {
		writeError(w, errors.New("invalid_origin"), http.StatusForbidden)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("X-EduForge-Token") != s.Token {
		writeError(w, errors.New("write_token_required"), http.StatusForbidden)
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), ownerKey{}, s.App.Store.LocalUser))
	defer func() {
		if recover() != nil {
			writeError(w, errors.New("internal_error"), 500)
		}
	}()
	s.Mux.ServeHTTP(w, r)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, err error, status int) {
	if errors.Is(err, domain.ErrNotFound) {
		status = 404
	} else if errors.Is(err, domain.ErrConflict) {
		status = 409
	}
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": err.Error()}})
}
func respond(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeError(w, err, 400)
		return
	}
	writeJSON(w, 200, value)
}
func decode(r *http.Request, w http.ResponseWriter, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return errors.New("invalid_request_json")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return errors.New("trailing_json")
	}
	return nil
}
func (s *Server) handle(pattern string, fn func(http.ResponseWriter, *http.Request) error) {
	s.Mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			writeError(w, err, 400)
		}
	})
}
func (s *Server) routes() {
	s.handle("GET /api/health", func(w http.ResponseWriter, r *http.Request) error {
		if err := s.App.Store.DB.PingContext(r.Context()); err != nil {
			return err
		}
		writeJSON(w, 200, map[string]any{"status": "ok", "version": "0.1.0"})
		return nil
	})
	s.handle("GET /api/session", func(w http.ResponseWriter, r *http.Request) error {
		writeJSON(w, 200, map[string]any{"owner_user_id": owner(r), "csrf_token": s.Token, "single_user": true})
		return nil
	})
	s.handle("GET /api/system", func(w http.ResponseWriter, r *http.Request) error {
		tools := map[string]bool{}
		for _, name := range []string{"pdftotext", "pdftoppm", "pdfimages", "libreoffice", "git", "ssh", "codex", "claude"} {
			_, err := exec.LookPath(name)
			tools[name] = err == nil
		}
		writeJSON(w, 200, map[string]any{"tools": tools, "container": os.Getenv("EDUFORGE_CONTAINER") == "1"})
		return nil
	})
	s.handle("GET /api/courses", func(w http.ResponseWriter, r *http.Request) error {
		items, err := s.App.Store.List(r.Context(), owner(r), store.Filter{Kind: "course"})
		respond(w, items, err)
		return nil
	})
	s.handle("POST /api/courses", func(w http.ResponseWriter, r *http.Request) error {
		var p domain.Course
		if err := decode(r, w, &p); err != nil {
			return err
		}
		out, err := s.App.CreateCourse(r.Context(), owner(r), p)
		respond(w, out, err)
		return nil
	})
	s.handle("PUT /api/courses/{id}", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Version     int                   `json:"version"`
			Name        string                `json:"name"`
			Description string                `json:"description"`
			Settings    domain.OutputSettings `json:"settings"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.SaveCourse(r.Context(), owner(r), r.PathValue("id"), input.Version, input.Name, input.Description, input.Settings)
		respond(w, out, err)
		return nil
	})
	s.handle("PUT /api/courses/{id}/order", func(w http.ResponseWriter, r *http.Request) error {
		var input app.CourseOrder
		if err := decode(r, w, &input); err != nil {
			return err
		}
		err := s.App.ReorderCourse(r.Context(), owner(r), r.PathValue("id"), input)
		respond(w, map[string]bool{"saved": err == nil}, err)
		return nil
	})
	s.handle("POST /api/courses/{id}/syllabus", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Weeks    int    `json:"weeks"`
			Language string `json:"language"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.QueueSyllabus(r.Context(), owner(r), r.PathValue("id"), input.Weeks, input.Language)
		respond(w, out, err)
		return nil
	})
	s.handle("GET /api/summary", func(w http.ResponseWriter, r *http.Request) error {
		out, err := s.App.Summary(r.Context(), owner(r), r.URL.Query().Get("course_id"))
		respond(w, out, err)
		return nil
	})
	s.handle("PUT /api/preferences", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.Preference(r.Context(), owner(r), input.Key, input.Value)
		respond(w, out, err)
		return nil
	})
	s.handle("DELETE /api/courses/{id}", func(w http.ResponseWriter, r *http.Request) error {
		if r.URL.Query().Get("confirm") != "delete-course-data" {
			return errors.New("deletion_confirmation_required")
		}
		err := s.App.DeleteCourse(r.Context(), owner(r), r.PathValue("id"))
		respond(w, map[string]bool{"deleted": err == nil}, err)
		return nil
	})
	s.handle("POST /api/courses/{id}/lessons", func(w http.ResponseWriter, r *http.Request) error {
		var p domain.Lesson
		if err := decode(r, w, &p); err != nil {
			return err
		}
		out, err := s.App.CreateLesson(r.Context(), owner(r), r.PathValue("id"), p)
		respond(w, out, err)
		return nil
	})
	s.handle("PUT /api/lessons/{id}", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Version int           `json:"version"`
			Lesson  domain.Lesson `json:"lesson"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.SaveLesson(r.Context(), owner(r), r.PathValue("id"), input.Version, input.Lesson)
		respond(w, out, err)
		return nil
	})
	s.handle("DELETE /api/lessons/{id}", func(w http.ResponseWriter, r *http.Request) error {
		if r.URL.Query().Get("confirm") != "delete-lesson-data" {
			return errors.New("deletion_confirmation_required")
		}
		err := s.App.DeleteLesson(r.Context(), owner(r), r.PathValue("id"))
		respond(w, map[string]bool{"deleted": err == nil}, err)
		return nil
	})
	s.handle("PUT /api/lessons/{id}/chapter", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Chapter string `json:"chapter"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.MoveLesson(r.Context(), owner(r), r.PathValue("id"), input.Chapter)
		respond(w, out, err)
		return nil
	})
	s.handle("POST /api/courses/{id}/chapters", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Name string `json:"name"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.CreateChapter(r.Context(), owner(r), r.PathValue("id"), input.Name)
		respond(w, out, err)
		return nil
	})
	s.handle("PUT /api/chapters/{id}", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Name     string                `json:"name"`
			Settings domain.OutputSettings `json:"settings"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.SaveChapter(r.Context(), owner(r), r.PathValue("id"), input.Name, input.Settings)
		respond(w, out, err)
		return nil
	})
	s.handle("DELETE /api/chapters/{id}", func(w http.ResponseWriter, r *http.Request) error {
		err := s.App.DeleteChapter(r.Context(), owner(r), r.PathValue("id"))
		respond(w, map[string]bool{"deleted": err == nil}, err)
		return nil
	})
	s.handle("POST /api/lessons/{id}/sources", s.upload)
	s.handle("POST /api/lessons/{id}/parse", func(w http.ResponseWriter, r *http.Request) error {
		out, err := s.App.QueueParse(r.Context(), owner(r), r.PathValue("id"))
		respond(w, out, err)
		return nil
	})
	s.handle("PUT /api/lessons/{id}/knowledge", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Version   int              `json:"version"`
			Knowledge domain.Knowledge `json:"knowledge"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.SaveKnowledge(r.Context(), owner(r), r.PathValue("id"), input.Version, input.Knowledge)
		respond(w, out, err)
		return nil
	})
	s.handle("POST /api/lessons/{id}/knowledge/ai", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Version    int  `json:"version"`
			MissedOnly bool `json:"missed_only"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.QueueKnowledge(r.Context(), owner(r), r.PathValue("id"), input.Version, input.MissedOnly)
		respond(w, out, err)
		return nil
	})
	s.handle("POST /api/templates", func(w http.ResponseWriter, r *http.Request) error {
		r.Body = http.MaxBytesReader(w, r.Body, 65<<20)
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			return errors.New("invalid_upload")
		}
		defer r.MultipartForm.RemoveAll()
		f, h, err := r.FormFile("file")
		if err != nil {
			return errors.New("file_required")
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
		if err != nil {
			return err
		}
		var out domain.Record
		if r.FormValue("kind") == "plan-form" {
			out, err = s.App.ImportPlanForm(r.Context(), owner(r), r.FormValue("name"), h.Filename, b)
		} else {
			out, err = s.App.ImportTemplate(r.Context(), owner(r), r.FormValue("name"), h.Filename, b)
		}
		respond(w, out, err)
		return nil
	})
	s.handle("POST /api/templates/extract", func(w http.ResponseWriter, r *http.Request) error {
		r.Body = http.MaxBytesReader(w, r.Body, 65<<20)
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			return errors.New("invalid_upload")
		}
		defer r.MultipartForm.RemoveAll()
		f, h, err := r.FormFile("file")
		if err != nil {
			return errors.New("file_required")
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
		if err != nil {
			return err
		}
		out, err := s.App.ExtractTemplate(r.Context(), owner(r), h.Filename, r.FormValue("language"), b)
		respond(w, out, err)
		return nil
	})
	s.handle("POST /api/templates/{id}/extract/confirm", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Version    int                   `json:"version"`
			Suggestion domain.DeckSuggestion `json:"suggestion"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.ConfirmExtraction(r.Context(), owner(r), r.PathValue("id"), input.Version, input.Suggestion)
		respond(w, out, err)
		return nil
	})
	s.handle("PUT /api/templates/{id}", s.saveTemplate)
	s.handle("GET /api/templates/{id}/spec", func(w http.ResponseWriter, r *http.Request) error {
		out, err := s.App.TemplateSpec(r.Context(), owner(r), r.PathValue("id"))
		respond(w, out, err)
		return nil
	})
	s.handle("PUT /api/templates/{id}/spec", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Version int                    `json:"version"`
			Spec    artifacts.TemplateSpec `json:"spec"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.SaveTemplateSpec(r.Context(), owner(r), r.PathValue("id"), input.Version, input.Spec)
		respond(w, out, err)
		return nil
	})
	s.handle("POST /api/templates/{id}/ai-edit", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Instruction string                 `json:"instruction"`
			Spec        artifacts.TemplateSpec `json:"spec"`
			Selection   []string               `json:"selection"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.SuggestTemplateEdits(r.Context(), owner(r), r.PathValue("id"), input.Instruction, input.Spec, input.Selection)
		respond(w, map[string]any{"changes": out}, err)
		return nil
	})
	s.handle("POST /api/templates/{id}/restore", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Version int `json:"version"`
			From    int `json:"from"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.RestoreTemplate(r.Context(), owner(r), r.PathValue("id"), input.Version, input.From)
		respond(w, out, err)
		return nil
	})
	s.handle("POST /api/providers", s.saveProvider)
	s.handle("PUT /api/providers/{id}", s.saveProvider)
	s.handle("POST /api/providers/{id}/login", func(w http.ResponseWriter, r *http.Request) error {
		result, err := s.App.Providers.StartLogin(r.Context(), owner(r), r.PathValue("id"))
		respond(w, result, err)
		return nil
	})
	s.handle("GET /api/providers/{id}/login", func(w http.ResponseWriter, r *http.Request) error {
		result, err := s.App.Providers.LoginStatus(r.Context(), owner(r), r.PathValue("id"))
		respond(w, result, err)
		return nil
	})
	s.handle("GET /api/providers/{id}/models", func(w http.ResponseWriter, r *http.Request) error {
		result, err := s.App.Providers.Models(r.Context(), owner(r), r.PathValue("id"))
		respond(w, result, err)
		return nil
	})
	s.handle("POST /api/providers/{id}/test-cli", func(w http.ResponseWriter, r *http.Request) error {
		err := s.App.Providers.VerifyCLI(r.Context(), owner(r), r.PathValue("id"))
		respond(w, map[string]bool{"inference_verified": err == nil}, err)
		return nil
	})
	s.handle("PUT /api/providers/order", s.providerOrder)
	s.handle("POST /api/courses/{id}/requests", func(w http.ResponseWriter, r *http.Request) error {
		var input domain.Request
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.CreateRequest(r.Context(), owner(r), r.PathValue("id"), input)
		respond(w, out, err)
		return nil
	})

	s.handle("POST /api/requests/{id}/start", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			UILanguage  string `json:"ui_language"`
			UseDefaults bool   `json:"use_defaults"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.StartRequest(r.Context(), owner(r), r.PathValue("id"), input.UILanguage, input.UseDefaults)
		respond(w, out, err)
		return nil
	})
	s.handle("POST /api/requests/{id}/control", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Action    string   `json:"action"`
			Budget    *float64 `json:"budget"`
			Unlimited bool     `json:"unlimited"`
			Refresh   bool     `json:"refresh_providers"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.ControlRequest(r.Context(), owner(r), r.PathValue("id"), input.Action, input.Budget, input.Unlimited, input.Refresh)
		respond(w, out, err)
		return nil
	})

	s.handle("POST /api/tasks/{id}/control", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Action string `json:"action"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.ControlTask(r.Context(), owner(r), r.PathValue("id"), input.Action)
		respond(w, out, err)
		return nil
	})
	s.handle("PUT /api/artifacts/{id}", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Version  int             `json:"version"`
			Artifact domain.Artifact `json:"artifact"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.EditArtifact(r.Context(), owner(r), r.PathValue("id"), input.Version, input.Artifact)
		respond(w, out, err)
		return nil
	})
	s.handle("DELETE /api/artifacts/{id}", func(w http.ResponseWriter, r *http.Request) error {
		n, err := s.App.DeleteArtifact(r.Context(), owner(r), r.PathValue("id"), r.URL.Query().Get("all") == "1")
		respond(w, map[string]int{"deleted": n}, err)
		return nil
	})
	s.handle("POST /api/artifacts/{id}/ai-edit", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Instruction string               `json:"instruction"`
			Blocks      []research.EditBlock `json:"blocks"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.SuggestEdits(r.Context(), owner(r), r.PathValue("id"), input.Instruction, input.Blocks)
		respond(w, map[string]any{"edits": out}, err)
		return nil
	})
	s.handle("GET /api/artifacts/{id}/download", s.download)
	s.handle("POST /api/artifacts/{id}/template", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			TemplateID string `json:"template_id"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		out, err := s.App.ReapplyTemplate(r.Context(), owner(r), r.PathValue("id"), input.TemplateID)
		respond(w, out, err)
		return nil
	})
	s.handle("GET /api/files/{id}", s.file)
	s.handle("POST /api/math", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Formulas []mathtex.Formula `json:"formulas"`
		}
		if err := decode(r, w, &input); err != nil {
			return err
		}
		if len(input.Formulas) > 500 {
			return errors.New("too_many_formulas")
		}
		out := make([]mathtex.Rendered, len(input.Formulas))
		if artifacts.Math == nil {
			for i := range out {
				out[i].Error = "math_renderer_unavailable"
			}
			writeJSON(w, 200, out)
			return nil
		}
		for i, f := range input.Formulas {
			if len(f.TeX) > 4000 {
				input.Formulas[i].TeX = ""
			}
		}
		rendered, err := artifacts.Math.Render(r.Context(), input.Formulas)
		if err != nil {
			return err
		}
		writeJSON(w, 200, rendered)
		return nil
	})
	s.handle("GET /api/courses/{id}/export", func(w http.ResponseWriter, r *http.Request) error {
		b, err := s.App.Export(r.Context(), owner(r), r.PathValue("id"), r.URL.Query().Get("personal") == "true")
		if err != nil {
			return err
		}
		attachment(w, "eduforge-course.zip", "application/zip", b)
		return nil
	})
	s.handle("POST /api/courses/import", s.importCourse)
	s.handle("DELETE /api/records/{id}", s.deleteRecord)
	s.Mux.HandleFunc("/", s.static)
}
func (s *Server) upload(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 65<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return errors.New("invalid_upload")
	}
	defer r.MultipartForm.RemoveAll()
	f, h, err := r.FormFile("file")
	if err != nil {
		return errors.New("file_required")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
	if err != nil {
		return err
	}
	out, err := s.App.Upload(r.Context(), owner(r), r.PathValue("id"), h.Filename, r.FormValue("role"), b)
	respond(w, out, err)
	return nil
}
func (s *Server) saveProvider(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Provider domain.Provider `json:"provider"`
		Key      string          `json:"key"`
	}
	if err := decode(r, w, &input); err != nil {
		return err
	}
	out, err := s.App.SaveProvider(r.Context(), owner(r), r.PathValue("id"), input.Provider, input.Key)
	respond(w, out, err)
	return nil
}
func (s *Server) providerOrder(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		IDs []string `json:"ids"`
	}
	if err := decode(r, w, &input); err != nil {
		return err
	}
	all, err := s.App.Store.List(r.Context(), owner(r), store.Filter{Kind: "provider"})
	if err != nil {
		return err
	}
	if len(all) != len(input.IDs) {
		return errors.New("provider_order_must_include_all")
	}
	seen := map[string]bool{}
	for _, id := range input.IDs {
		if seen[id] {
			return errors.New("duplicate_provider")
		}
		seen[id] = true
		if _, err := s.App.Store.Get(r.Context(), owner(r), id, "provider"); err != nil {
			return err
		}
	}
	items, err := s.App.Store.List(r.Context(), owner(r), store.Filter{Kind: "preference"})
	if err != nil {
		return err
	}
	for _, item := range items {
		var p map[string]any
		_ = json.Unmarshal(item.Data, &p)
		if p["key"] == "provider_order" {
			_, err = s.App.Store.Update(r.Context(), owner(r), item.ID, 0, func(v *domain.Record) error {
				v.Data = domain.Encode(map[string]any{"key": "provider_order", "ids": input.IDs})
				return nil
			})
			respond(w, input, err)
			return nil
		}
	}
	_, err = s.App.Store.Create(r.Context(), domain.Record{Owner: owner(r), Kind: "preference", Data: domain.Encode(map[string]any{"key": "provider_order", "ids": input.IDs})})
	respond(w, input, err)
	return nil
}
func (s *Server) saveTemplate(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Version  int             `json:"version"`
		Template domain.Template `json:"template"`
	}
	if err := decode(r, w, &input); err != nil {
		return err
	}
	out, err := s.App.Store.Update(r.Context(), owner(r), r.PathValue("id"), input.Version, func(row *domain.Record) error {
		if row.Kind != "template" {
			return domain.ErrNotFound
		}
		// An imported template's design comes from its file; only the name changes.
		old, err := domain.Decode[domain.Template](row.Data)
		if err != nil {
			return err
		}
		if name := strings.TrimSpace(input.Template.Name); name != "" {
			old.Name = name
		}
		row.Data = domain.Encode(old)
		return nil
	})
	respond(w, out, err)
	return nil
}
func (s *Server) deleteRecord(w http.ResponseWriter, r *http.Request) error {
	record, err := s.App.Store.Get(r.Context(), owner(r), r.PathValue("id"), "")
	if err != nil {
		return err
	}
	if record.Kind != "reading" && record.Kind != "feedback" && record.Kind != "profile" && record.Kind != "template" && record.Kind != "provider" && record.Kind != "plan_chat" {
		return errors.New("record_deletion_not_allowed")
	}
	if record.Kind == "provider" {
		if err = s.App.Providers.DeleteSecret(owner(r), record.ID); err != nil {
			return err
		}
	}
	err = s.App.Store.Delete(r.Context(), owner(r), record.ID)
	respond(w, map[string]bool{"deleted": err == nil}, err)
	return nil
}
func attachment(w http.ResponseWriter, name, typ string, b []byte) {
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}
func (s *Server) download(w http.ResponseWriter, r *http.Request) error {
	record, err := s.App.Store.Get(r.Context(), owner(r), r.PathValue("id"), "artifact")
	if err != nil {
		return err
	}
	a, err := domain.Decode[domain.Artifact](record.Data)
	if err != nil {
		return err
	}
	format := r.URL.Query().Get("format")
	if a.Type == "C" || a.Type == "D" || a.Type == "S" {
		var form []byte
		if a.Type == "C" {
			if form, err = s.App.ArtifactPlanForm(r.Context(), owner(r), a); err != nil {
				return err
			}
		}
		b, typ, name, err := artifacts.StructuredExport(r.Context(), a, format, r.URL.Query().Get("version"), form)
		if err != nil {
			return err
		}
		if format == "html" && r.URL.Query().Get("preview") == "true" {
			w.Header().Set("Content-Type", typ)
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Frame-Options", "SAMEORIGIN")
			w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'self'")
			_, _ = w.Write(b)
			return nil
		}
		attachment(w, name, typ, b)
		return nil
	}
	k, err := s.App.ArtifactKnowledge(r.Context(), owner(r), a)
	if err != nil {
		return err
	}
	var b []byte
	typ := ""
	switch format {
	case "html":
		b, err = artifacts.HTML(s.App.Store, a, k)
		typ = "text/html; charset=utf-8"
	case "pdf":
		b, err = artifacts.PDF(r.Context(), s.App.Store, a, k)
		typ = "application/pdf"
	case "notes":
		b, err = artifacts.NotesPDF(r.Context(), s.App.Store, a, k)
		typ = "application/pdf"
	case "docx":
		b, err = artifacts.DOCX(r.Context(), s.App.Store, a, k)
		typ = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "pptx":
		if a.Type != "B" {
			return errors.New("presentation_required")
		}
		b, err = artifacts.PPTX(s.App.Store, a, k)
		typ = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case "json":
		b = domain.Encode(record)
		typ = "application/json"
	default:
		return errors.New("unsupported_export_format")
	}
	if err != nil {
		return err
	}
	if format == "html" && r.URL.Query().Get("preview") == "true" {
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src data:; frame-ancestors 'self'")
		_, _ = w.Write(b)
		return nil
	}
	name := "EduForge-" + a.Type + "-" + a.Language + "." + format
	if format == "notes" {
		name = "EduForge-" + a.Type + "-" + a.Language + "-notes.pdf"
	}
	attachment(w, name, typ, b)
	return nil
}
func (s *Server) file(w http.ResponseWriter, r *http.Request) error {
	record, err := s.App.Store.Get(r.Context(), owner(r), r.PathValue("id"), "")
	if err != nil {
		return err
	}
	rel := r.URL.Query().Get("path")
	if !app.BlobReferences(record)[rel] {
		return domain.ErrNotFound
	}
	owned, err := s.App.OwnedBlobs(r.Context(), owner(r))
	if err != nil {
		return err
	}
	if !owned[rel] {
		return domain.ErrNotFound
	}
	file, err := s.App.Store.Path(rel)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, file)
	return nil
}
func (s *Server) importCourse(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 129<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return errors.New("invalid_upload")
	}
	defer r.MultipartForm.RemoveAll()
	f, _, err := r.FormFile("file")
	if err != nil {
		return errors.New("file_required")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 128<<20))
	if err != nil {
		return err
	}
	out, err := s.App.Import(r.Context(), owner(r), b, r.FormValue("personal") == "true")
	respond(w, out, err)
	return nil
}
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, domain.ErrNotFound, 404)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(405)
		return
	}
	clean := filepath.Clean("/" + r.URL.Path)
	file := filepath.Join(s.Web, strings.TrimPrefix(clean, "/"))
	if stat, err := os.Stat(file); err == nil && !stat.IsDir() {
		http.ServeFile(w, r, file)
		return
	}
	index := filepath.Join(s.Web, "index.html")
	if _, err := os.Stat(index); err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(503)
		_, _ = fmt.Fprint(w, "EduForge API is running. Build the Vue frontend with npm run build in frontend/.")
		return
	}
	http.ServeFile(w, r, index)
}

func DefaultOrigins(listen string) []string {
	port := "8080"
	if at := strings.LastIndex(listen, ":"); at >= 0 {
		port = listen[at+1:]
	}
	out := []string{"http://127.0.0.1:" + port, "http://localhost:" + port}
	if custom := os.Getenv("EDUFORGE_ORIGINS"); custom != "" {
		out = strings.Split(custom, ",")
	}
	return out
}
func HTTPServer(handler http.Handler, listen string) *http.Server {
	return &http.Server{Addr: listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
}
