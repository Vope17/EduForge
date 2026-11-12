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

const oauthCookie = "eduforge_oauth_binding"
const callbackPath = "/api/v1/model-connection/chatgpt/callback"

type ModelConnections struct {
	manager *llm.Connections
	oauth   *llm.OAuth
	db      *store.Store
	limit   int
	csrf    string
}

func NewModelConnections(manager *llm.Connections, oauth *llm.OAuth, db *store.Store, limit int) *ModelConnections {
	return &ModelConnections{manager: manager, oauth: oauth, db: db, limit: limit, csrf: artifact.NewID()}
}

func (c *ModelConnections) register(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/model-connection", c.settings)
	mux.HandleFunc("/api/v1/model-connection/test", c.test)
	mux.HandleFunc("/api/v1/model-connection/models", c.models)
	mux.HandleFunc("/api/v1/model-connection/chatgpt/login", c.login)
	mux.HandleFunc(callbackPath, c.callback)
}

type connectionDTO struct {
	Connected     bool   `json:"connected"`
	Mode          string `json:"mode"`
	Source        string `json:"source"`
	Endpoint      string `json:"endpoint"`
	Model         string `json:"model"`
	Account       string `json:"account"`
	Verified      bool   `json:"verified"`
	Version       uint64 `json:"version"`
	CSRFToken     string `json:"csrf_token"`
	ChatGPTOrigin string `json:"chatgpt_origin"`
}

func (c *ModelConnections) status() connectionDTO {
	current := c.manager.Status()
	return connectionDTO{
		Connected: current.Connected, Mode: current.Mode, Source: current.Source,
		Endpoint: current.Endpoint, Model: current.Model, Account: current.Account, Verified: current.Verified,
		Version: current.Version, CSRFToken: c.csrf, ChatGPTOrigin: c.oauth.Origin(),
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
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
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
		c.oauth.Cancel()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		revoked, err := c.manager.Disconnect(ctx, input.Version)
		result := struct {
			Connection    connectionDTO `json:"connection"`
			RemoteRevoked bool          `json:"remote_revoked"`
		}{c.status(), revoked}
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
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	err := c.manager.Test(ctx, input.Version)
	connectionRespond(w, r, c.status(), err)
}

func (c *ModelConnections) models(w http.ResponseWriter, r *http.Request) {
	if !methodAllowed(w, r, http.MethodGet) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	models, err := c.manager.Models(ctx)
	connectionRespond(w, r, models, err)
}

func (c *ModelConnections) login(w http.ResponseWriter, r *http.Request) {
	if !methodAllowed(w, r, http.MethodPost) || !c.authorized(w, r) {
		return
	}
	var input struct {
		NewAccount     bool `json:"new_account"`
		RequestConsent bool `json:"request_consent"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	authorizationURL, binding, err := c.oauth.Start(ctx, defaultUser, input.NewAccount, input.RequestConsent)
	if err != nil {
		connectionRespond(w, r, nil, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Value: binding, Path: callbackPath, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: 600,
	})
	connectionRespond(w, r, struct {
		URL string `json:"authorization_url"`
	}{authorizationURL}, nil)
}

func (c *ModelConnections) callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !methodAllowed(w, r, http.MethodGet) {
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Path: callbackPath, MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	binding, cookieErr := r.Cookie(oauthCookie)
	result := "expired"
	if cookieErr == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		err := c.oauth.Complete(ctx, defaultUser, binding.Value, r.URL.Query())
		switch {
		case err == nil:
			result = "success"
		case errors.Is(err, llm.ErrAuthorizationDeny):
			result = "denied"
		case errors.Is(err, llm.ErrPlanPermission):
			result = "permission"
		case errors.Is(err, llm.ErrOAuthState):
			result = "expired"
		default:
			result = "failed"
		}
	}
	http.Redirect(w, r, c.oauth.Origin()+"/settings/models?oauth="+result, http.StatusSeeOther)
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
