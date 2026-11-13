package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"eduforge/backend/internal/artifact"
	"eduforge/backend/internal/generate"
	"eduforge/backend/internal/job"
	"eduforge/backend/internal/llm"
	"eduforge/backend/internal/store"
	"eduforge/backend/internal/template"
)

const defaultUser = "default"

type Documents struct {
	kind       artifact.Type
	db         *store.Store
	generation *generate.Service
	jobs       *job.Manager
}

func NewDocuments(db *store.Store, generation *generate.Service, jobs *job.Manager) *Documents {
	return &Documents{db: db, generation: generation, kind: artifact.DocumentType, jobs: jobs}
}

func (d *Documents) register(mux *http.ServeMux) {
	if d.jobs != nil {
		mux.HandleFunc("/api/v1/jobs/{id}", d.getJob)
		mux.HandleFunc("/api/v1/jobs/{id}/cancel", d.cancelJob)
	}
	for _, kind := range []artifact.Type{artifact.DocumentType, artifact.SlidesType} {
		resource := *d
		resource.kind = kind
		prefix := "/api/v1/documents"
		if kind == artifact.SlidesType {
			prefix = "/api/v1/slides"
			mux.HandleFunc(prefix+"/{id}/images", resource.checkType(resource.uploadImage))
			mux.HandleFunc(prefix+"/{id}/images/{image}", resource.checkType(resource.image))
		}
		mux.HandleFunc(prefix, resource.collection)
		for _, route := range []struct {
			path    string
			handler http.HandlerFunc
		}{
			{"", resource.get},
			{"/preview", resource.preview},
			{"/confirm", resource.confirm},
			{"/generate", resource.generate},
			{"/revisions", resource.revisions},
			{"/download", resource.download},
			{"/patches", resource.propose},
			{"/patches/{proposal}/accept", resource.acceptProposal},
			{"/patches/{proposal}/reject", resource.rejectProposal},
			{"/revisions/{revision}", resource.revision},
		} {
			mux.HandleFunc(prefix+"/{id}"+route.path, resource.checkType(route.handler))
		}
	}
}

func (d *Documents) checkType(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := d.db.Request(r.Context(), defaultUser, r.PathValue("id"))
		if err == nil && request.Type != d.kind {
			err = store.ErrNotFound
		}
		if err != nil {
			respond(w, r, 0, nil, err)
			return
		}
		next(w, r)
	}
}

type requestDTO struct {
	Type             artifact.Type     `json:"artifact_type"`
	SlideCount       int               `json:"slide_count"`
	ID               string            `json:"id"`
	State            template.State    `json:"state"`
	Language         artifact.Language `json:"language"`
	Topic            string            `json:"topic"`
	Audience         string            `json:"audience"`
	Kind             string            `json:"kind"`
	Version          int               `json:"template_version"`
	ConfirmedVersion int               `json:"confirmed_version"`
	Template         *template.Spec    `json:"template,omitempty"`
}

func requestResponse(r template.Request) requestDTO {
	return requestDTO{
		Type: r.Type, SlideCount: r.SlideCount, ID: r.ID, State: r.State, Language: r.Language, Topic: r.Topic, Audience: r.Audience,
		Kind: r.Kind, Version: r.Version, ConfirmedVersion: r.ConfirmedVersion, Template: r.Spec,
	}
}

type documentDTO struct {
	Type     artifact.Type     `json:"artifact_type"`
	ID       string            `json:"id"`
	Language artifact.Language `json:"language"`
	Revision int               `json:"revision"`
	Content  artifact.Content  `json:"content"`
	CanUndo  bool              `json:"can_undo"`
	CanRedo  bool              `json:"can_redo"`
}

func documentResponse(d artifact.Artifact) documentDTO {
	return documentDTO{Type: d.Type, ID: d.ID, Language: d.Language, Revision: d.Revision, Content: d.Content, CanUndo: d.CanUndo, CanRedo: d.CanRedo}
}

type summaryDTO struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	State     template.State `json:"state"`
	CreatedAt string         `json:"created_at"`
}

func (d *Documents) collection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		items, err := d.db.ListRequests(r.Context(), defaultUser, d.kind)
		result := make([]summaryDTO, 0, len(items))
		for _, item := range items {
			result = append(result, summaryDTO{item.ID, item.Title, item.State, item.CreatedAt})
		}
		respond(w, r, http.StatusOK, result, err)
	case http.MethodPost:
		var input generate.Input
		if !readJSON(w, r, &input) {
			return
		}
		input.Type = d.kind
		value, err := d.generation.Create(r.Context(), defaultUser, input)
		respond(w, r, http.StatusCreated, requestResponse(value), err)
	default:
		methodAllowed(w, r, http.MethodGet, http.MethodHead, http.MethodPost)
	}
}

func (d *Documents) get(w http.ResponseWriter, r *http.Request) {
	if !methodAllowed(w, r, http.MethodGet, http.MethodHead) {
		return
	}
	id := r.PathValue("id")
	value, err := d.db.Request(r.Context(), defaultUser, id)
	result := struct {
		Request  requestDTO   `json:"request"`
		Document *documentDTO `json:"document,omitempty"`
		Job      *jobDTO      `json:"job,omitempty"`
	}{Request: requestResponse(value)}
	if err == nil && value.State == template.Ready {
		document, documentErr := d.db.Document(r.Context(), defaultUser, id, 0)
		dto := documentResponse(document)
		result.Document = &dto
		err = documentErr
	}
	if err == nil && d.kind == artifact.SlidesType {
		latest, jobErr := d.db.LatestGeneration(r.Context(), defaultUser, id)
		if jobErr == nil {
			dto := jobResponse(latest)
			result.Job = &dto
		} else if !errors.Is(jobErr, store.ErrNotFound) {
			err = jobErr
		}
	}
	respond(w, r, http.StatusOK, result, err)
}

func (d *Documents) preview(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Version  int    `json:"template_version"`
		Feedback string `json:"feedback"`
	}
	if !methodAllowed(w, r, http.MethodPost) || !readJSON(w, r, &input) {
		return
	}
	value, err := d.generation.Preview(r.Context(), defaultUser, r.PathValue("id"), input.Version, input.Feedback)
	respond(w, r, http.StatusOK, requestResponse(value), err)
}

type versionInput struct {
	Version int `json:"template_version"`
}

func (d *Documents) confirm(w http.ResponseWriter, r *http.Request) {
	var input versionInput
	if !methodAllowed(w, r, http.MethodPost) || !readJSON(w, r, &input) {
		return
	}
	value, err := d.generation.Confirm(r.Context(), defaultUser, r.PathValue("id"), input.Version)
	respond(w, r, http.StatusOK, requestResponse(value), err)
}

func (d *Documents) generate(w http.ResponseWriter, r *http.Request) {
	var input versionInput
	if !methodAllowed(w, r, http.MethodPost) || !readJSON(w, r, &input) {
		return
	}
	if d.kind == artifact.SlidesType && d.jobs != nil {
		value, err := d.jobs.Submit(r.Context(), defaultUser, r.PathValue("id"), input.Version)
		respond(w, r, http.StatusAccepted, jobResponse(value), err)
		return
	}
	value, err := d.generation.Generate(r.Context(), defaultUser, r.PathValue("id"), input.Version)
	respond(w, r, http.StatusCreated, documentResponse(value), err)
}

type revisionDTO struct {
	Number    int    `json:"revision"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
}

func (d *Documents) revisions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		items, err := d.db.Revisions(r.Context(), defaultUser, id)
		result := make([]revisionDTO, 0, len(items))
		for _, item := range items {
			result = append(result, revisionDTO{item.Number, item.Title, item.CreatedAt})
		}
		respond(w, r, http.StatusOK, result, err)
	case http.MethodPost:
		var input struct {
			BaseRevision   int               `json:"base_revision"`
			Action         string            `json:"action"`
			Content        *artifact.Content `json:"content"`
			TargetRevision int               `json:"target_revision"`
		}
		if !readJSON(w, r, &input) {
			return
		}
		if input.BaseRevision < 1 || input.Action == "restore" && input.TargetRevision < 1 {
			respond(w, r, 0, nil, artifact.ErrInvalid)
			return
		}
		value, err := d.db.ChangeDocument(r.Context(), defaultUser, id, store.Change{
			BaseRevision: input.BaseRevision, Action: input.Action, Content: input.Content, TargetRevision: input.TargetRevision,
		})
		respond(w, r, http.StatusCreated, documentResponse(value), err)
	default:
		methodAllowed(w, r, http.MethodGet, http.MethodHead, http.MethodPost)
	}
}

func (d *Documents) revision(w http.ResponseWriter, r *http.Request) {
	if !methodAllowed(w, r, http.MethodGet, http.MethodHead) {
		return
	}
	number, err := strconv.Atoi(r.PathValue("revision"))
	if err != nil || number < 1 {
		respond(w, r, 0, nil, store.ErrNotFound)
		return
	}
	value, err := d.db.Document(r.Context(), defaultUser, r.PathValue("id"), number)
	respond(w, r, http.StatusOK, documentResponse(value), err)
}

func readJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large.")
		} else {
			writeError(w, r, http.StatusBadRequest, "invalid_input", "Invalid request JSON.")
		}
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeError(w, r, http.StatusBadRequest, "invalid_input", "Expected one JSON value.")
		return false
	}
	return true
}

func methodAllowed(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	if slices.Contains(methods, r.Method) {
		return true
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method is not allowed.")
	return false
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, r, status, errorResponse{Error: errorDetail{Code: code, Message: message}})
}

func respond(w http.ResponseWriter, r *http.Request, status int, result any, err error) {
	switch {
	case err == nil:
		writeJSON(w, r, status, result)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "Document or revision not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, r, http.StatusConflict, "version_conflict", "Refresh the document and review the current version.")
	case errors.Is(err, store.ErrLimit):
		writeError(w, r, http.StatusTooManyRequests, "generation_limit", "Daily generation limit reached.")
	case errors.Is(err, artifact.ErrSelection):
		writeError(w, r, http.StatusBadRequest, "invalid_selection", "The proposed change exceeds the selected scope or content limits.")
	case errors.Is(err, generate.ErrInput), errors.Is(err, artifact.ErrInvalid):
		writeError(w, r, http.StatusBadRequest, "invalid_input", "Check the document fields and limits.")
	case errors.Is(err, llm.ErrPlanPermission):
		writeError(w, r, http.StatusForbidden, "plan_permission", "Enable ChatGPT plan usage for this app.")
	case errors.Is(err, llm.ErrSubscriptionLimit):
		writeError(w, r, http.StatusTooManyRequests, "subscription_limit", "The ChatGPT plan usage limit has been reached.")
	case errors.Is(err, llm.ErrUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "provider_unavailable", "The generation provider is not configured.")
	case errors.Is(err, llm.ErrInvalidOutput):
		writeError(w, r, http.StatusBadGateway, "invalid_generation", "The generation provider returned invalid content.")
	case errors.Is(err, generate.ErrUpstream):
		writeError(w, r, http.StatusBadGateway, "generation_failed", "Generation failed. Please try again.")
	default:
		slog.ErrorContext(r.Context(), "document operation failed", "method", r.Method)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Document operation failed.")
	}
}

type jobDTO struct {
	ID              string `json:"job_id"`
	ArtifactID      string `json:"artifact_id"`
	State           string `json:"state"`
	Progress        int    `json:"progress"`
	ErrorCode       string `json:"error_code"`
	CancelRequested bool   `json:"cancel_requested"`
}

func jobResponse(job store.GenerationJob) jobDTO {
	return jobDTO{job.ID, job.ArtifactID, job.State, job.Progress, job.ErrorCode, job.CancelRequested}
}

func (d *Documents) getJob(w http.ResponseWriter, r *http.Request) {
	if !methodAllowed(w, r, http.MethodGet) {
		return
	}
	value, err := d.db.GenerationJob(r.Context(), defaultUser, r.PathValue("id"))
	respond(w, r, http.StatusOK, jobResponse(value), err)
}

func (d *Documents) cancelJob(w http.ResponseWriter, r *http.Request) {
	if !methodAllowed(w, r, http.MethodPost) {
		return
	}
	var input struct{}
	if !readJSON(w, r, &input) {
		return
	}
	value, err := d.jobs.Cancel(r.Context(), defaultUser, r.PathValue("id"))
	respond(w, r, http.StatusOK, jobResponse(value), err)
}

func (d *Documents) propose(w http.ResponseWriter, r *http.Request) {
	var input struct {
		BaseRevision int               `json:"base_revision"`
		Instruction  string            `json:"instruction"`
		Selection    []artifact.Anchor `json:"selection"`
	}
	if !methodAllowed(w, r, http.MethodPost) || !readJSON(w, r, &input) {
		return
	}
	value, err := d.generation.Propose(r.Context(), defaultUser, r.PathValue("id"), input.BaseRevision, input.Instruction, input.Selection)
	respond(w, r, http.StatusOK, value, err)
}

func (d *Documents) acceptProposal(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !methodAllowed(w, r, http.MethodPost) || !readJSON(w, r, &input) {
		return
	}
	value, err := d.db.AcceptProposal(r.Context(), defaultUser, r.PathValue("id"), r.PathValue("proposal"))
	respond(w, r, http.StatusCreated, documentResponse(value), err)
}

func (d *Documents) rejectProposal(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !methodAllowed(w, r, http.MethodPost) || !readJSON(w, r, &input) {
		return
	}
	err := d.db.RejectProposal(r.Context(), defaultUser, r.PathValue("id"), r.PathValue("proposal"))
	respond(w, r, http.StatusOK, struct{}{}, err)
}
