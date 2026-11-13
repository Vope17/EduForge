package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"eduforge/backend/internal/artifact"
	"eduforge/backend/internal/llm"
	"eduforge/backend/internal/store"
)

type ModelConnections struct {
	manager *llm.Connections
	db      *store.Store
	limit   int
	csrf    string
}

func NewModelConnections(manager *llm.Connections, db *store.Store, limit int) *ModelConnections {
	return &ModelConnections{manager: manager, db: db, limit: limit, csrf: artifact.NewID()}
}

// modelPreferences stores the chosen ChatGPT model for the single local user.
type modelPreferences struct{ db *store.Store }

func ModelPreferences(db *store.Store) llm.Preferences { return modelPreferences{db} }

func (p modelPreferences) ChatGPTModel(ctx context.Context) (string, error) {
	return p.db.ChatGPTModel(ctx, defaultUser)
}

func (p modelPreferences) SaveChatGPTModel(ctx context.Context, model string) error {
	return p.db.SaveChatGPTModel(ctx, defaultUser, model)
}

func (c *ModelConnections) register(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/model-connection", c.settings)
	mux.HandleFunc("/api/v1/model-connection/test", c.test)
	mux.HandleFunc("/api/v1/model-connection/models", c.models)
	mux.HandleFunc("/api/v1/model-connection/chatgpt/login", c.login)
}

type connectionDTO struct {
	Connected        bool   `json:"connected"`
	Mode             string `json:"mode"`
	Source           string `json:"source"`
	Endpoint         string `json:"endpoint"`
	Model            string `json:"model"`
	Account          string `json:"account"`
	Verified         bool   `json:"verified"`
	Version          uint64 `json:"version"`
	CSRFToken        string `json:"csrf_token"`
	ChatGPTAvailable bool   `json:"chatgpt_available"`
}

func (c *ModelConnections) status() connectionDTO {
	current := c.manager.Status()
	return connectionDTO{
		Connected: current.Connected, Mode: current.Mode, Source: current.Source,
		Endpoint: current.Endpoint, Model: current.Model, Account: current.Account, Verified: current.Verified,
		Version: current.Version, CSRFToken: c.csrf, ChatGPTAvailable: c.manager.ChatGPTAvailable(),
	}
}

func (c *ModelConnections) settings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		writeJSON(w, r, http.StatusOK, c.status())
		return
	}
	if !methodAllowed(w, r, http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete) ||
		!c.authorized(w, r) {
		return
	}
	switch r.Method {
	case http.MethodPut:
		var input struct {
			Endpoint string `json:"endpoint"`
			Model    string `json:"model"`
			APIKey   string `json:"api_key"`
			Version  uint64 `json:"version"`
		}
		if !readJSON(w, r, &input) {
			return
		}
		err := c.manager.SetAPIKey(input.Endpoint, input.Model, input.APIKey, input.Version)
		connectionRespond(w, r, c.status(), err)
	case http.MethodPatch:
		var input struct {
			Model   string `json:"model"`
			Version uint64 `json:"version"`
		}
		if !readJSON(w, r, &input) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		err := c.manager.SelectModel(ctx, input.Model, input.Version)
		connectionRespond(w, r, c.status(), err)
	case http.MethodDelete:
		var input struct {
			Version uint64 `json:"version"`
		}
		if !readJSON(w, r, &input) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		signedOut, err := c.manager.Disconnect(ctx, input.Version)
		result := struct {
			Connection connectionDTO `json:"connection"`
			SignedOut  bool          `json:"signed_out"`
		}{c.status(), signedOut}
		connectionRespond(w, r, result, err)
	}
}

func (c *ModelConnections) authorized(w http.ResponseWriter, r *http.Request) bool {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-EduForge-CSRF")), []byte(c.csrf)) != 1 ||
		!strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, r, http.StatusForbidden, "connection_forbidden", "Reload model settings and try again.")
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		host := r.Header.Get("X-Forwarded-Host")
		if host == "" {
			host = r.Host
		}
		if err != nil || parsed.Host != host || parsed.Scheme != "http" && parsed.Scheme != "https" ||
			parsed.Path != "" || parsed.RawQuery != "" || parsed.User != nil {
			writeError(w, r, http.StatusForbidden, "connection_forbidden", "This request must come from EduForge.")
			return false
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	return true
}

func (c *ModelConnections) test(w http.ResponseWriter, r *http.Request) {
	if !methodAllowed(w, r, http.MethodPost) || !c.authorized(w, r) {
		return
	}
	var input struct {
		Version uint64 `json:"version"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if !c.manager.Ready() {
		connectionRespond(w, r, nil, llm.ErrUnavailable)
		return
	}
	if err := c.db.ReserveGeneration(r.Context(), defaultUser, c.limit); err != nil {
		connectionRespond(w, r, nil, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	err := c.manager.Test(ctx, input.Version)
	connectionRespond(w, r, c.status(), err)
}

func (c *ModelConnections) models(w http.ResponseWriter, r *http.Request) {
	if !methodAllowed(w, r, http.MethodGet) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	models, err := c.manager.Models(ctx)
	connectionRespond(w, r, models, err)
}

type loginDTO struct {
	Status   string `json:"status"`
	UserCode string `json:"user_code,omitempty"`
	URL      string `json:"verification_url,omitempty"`
	Expires  string `json:"expires_at,omitempty"`
}

func newLoginDTO(state llm.LoginState) loginDTO {
	result := loginDTO{Status: state.Status, UserCode: state.UserCode, URL: state.URL}
	if !state.Expires.IsZero() {
		result.Expires = state.Expires.UTC().Format(time.RFC3339)
	}
	return result
}

// login starts (POST) or reports (GET) the ChatGPT device-code sign-in.
func (c *ModelConnections) login(w http.ResponseWriter, r *http.Request) {
	if !methodAllowed(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, r, http.StatusOK, newLoginDTO(c.manager.LoginStatus()))
		return
	}
	if !c.authorized(w, r) {
		return
	}
	var input struct{}
	if !readJSON(w, r, &input) {
		return
	}
	state, err := c.manager.StartLogin()
	connectionRespond(w, r, newLoginDTO(state), err)
}

func connectionRespond(w http.ResponseWriter, r *http.Request, result any, err error) {
	switch {
	case err == nil:
		writeJSON(w, r, http.StatusOK, result)
	case errors.Is(err, llm.ErrConfiguration):
		writeError(w, r, http.StatusBadRequest, "invalid_connection", "Check endpoint, model, and API key.")
	case errors.Is(err, llm.ErrConnectionChanged):
		writeError(w, r, http.StatusConflict, "connection_changed", "The connection changed. Reload settings.")
	case errors.Is(err, llm.ErrUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "provider_unavailable", "Configure or sign in to the model service.")
	case errors.Is(err, llm.ErrPlanPermission):
		writeError(w, r, http.StatusForbidden, "plan_permission", "Enable ChatGPT plan usage for this app.")
	case errors.Is(err, llm.ErrSubscriptionLimit), errors.Is(err, store.ErrLimit):
		writeError(w, r, http.StatusTooManyRequests, "connection_limit", "The model usage limit has been reached.")
	default:
		writeError(w, r, http.StatusBadGateway, "connection_failed", "The model service request could not be completed.")
	}
}
