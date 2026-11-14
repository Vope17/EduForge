package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"eduforge/internal/domain"
	"strings"
	"time"
)

type ModelOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type LoginState struct {
	Status  string `json:"status"`
	Code    string `json:"user_code,omitempty"`
	URL     string `json:"verification_url,omitempty"`
	Expires string `json:"expires_at,omitempty"`
	Error   string `json:"error,omitempty"`
}
type rpcClient struct {
	cmd   *exec.Cmd
	input io.WriteCloser
	enc   *json.Encoder
	dec   *json.Decoder
	next  int
}

func (m *Manager) codexClient(ctx context.Context) (*rpcClient, error) {
	cmd := exec.CommandContext(ctx, "codex", "app-server", "--listen", "stdio://")
	cmd.Dir = m.Store.Root
	cmd.Env = CLIEnvironment()
	cmd.Stderr = io.Discard
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		return nil, errors.New("codex_cli_not_available")
	}
	c := &rpcClient{cmd: cmd, input: input, enc: json.NewEncoder(input), dec: json.NewDecoder(output)}
	if err = c.call("initialize", map[string]any{"clientInfo": map[string]string{"name": "eduforge", "version": "0.1.0"}}, nil); err != nil {
		c.close()
		return nil, err
	}
	if err = c.enc.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}
func (c *rpcClient) close() { c.input.Close(); _ = c.cmd.Process.Kill(); _ = c.cmd.Wait() }
func (c *rpcClient) call(method string, params, result any) error {
	c.next++
	id := c.next
	if err := c.enc.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		var msg struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := c.dec.Decode(&msg); err != nil {
			return errors.New("codex_connection_failed")
		}
		if msg.ID != id {
			continue
		}
		if len(msg.Error) > 0 && string(msg.Error) != "null" {
			return errors.New("codex_connection_request_failed")
		}
		if result != nil {
			return json.Unmarshal(msg.Result, result)
		}
		return nil
	}
}
func (m *Manager) connectionProvider(ctx context.Context, owner, id string) (domain.Provider, error) {
	row, err := m.Store.Get(ctx, owner, id, "provider")
	if err != nil {
		return domain.Provider{}, err
	}
	p, err := domain.Decode[domain.Provider](row.Data)
	if (p.Kind == "codex" || p.Kind == "claude-cli") && owner != m.Store.LocalUser {
		return p, errors.New("cli_credentials_not_configured_for_user")
	}
	return p, err
}
func (m *Manager) StartLogin(ctx context.Context, owner, id string) (LoginState, error) {
	p, err := m.connectionProvider(ctx, owner, id)
	if err != nil {
		return LoginState{}, err
	}
	if p.Kind != "codex" {
		return LoginState{}, errors.New("openai_subscription_required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := owner + "/" + id
	if m.logins == nil {
		m.logins = map[string]LoginState{}
	}
	if state, ok := m.logins[key]; ok && state.Status == "pending" {
		return state, nil
	}
	loginCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	startup := time.AfterFunc(30*time.Second, cancel)
	defer startup.Stop()
	c, err := m.codexClient(loginCtx)
	if err != nil {
		cancel()
		return LoginState{}, err
	}
	var result struct {
		UserCode string `json:"userCode"`
		URL      string `json:"verificationUrl"`
	}
	if err = c.call("account/login/start", map[string]string{"type": "chatgptDeviceCode"}, &result); err != nil {
		c.close()
		cancel()
		return LoginState{}, err
	}
	u, e := url.Parse(result.URL)
	if e != nil || u.Scheme != "https" || u.Hostname() != "auth.openai.com" || result.UserCode == "" {
		c.close()
		cancel()
		return LoginState{}, errors.New("invalid_official_login_response")
	}
	state := LoginState{Status: "pending", Code: result.UserCode, URL: result.URL, Expires: time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339)}
	m.logins[key] = state
	go func() {
		defer cancel()
		defer c.close()
		completed := false
		for {
			var msg struct {
				Method string `json:"method"`
				Params struct {
					Success bool `json:"success"`
				} `json:"params"`
			}
			if c.dec.Decode(&msg) != nil {
				break
			}
			if msg.Method == "account/login/completed" {
				completed = msg.Params.Success
				break
			}
		}
		next := LoginState{Status: "failed", Error: "subscription_login_failed_or_expired"}
		if completed {
			next = LoginState{Status: "connected"}
		}
		m.mu.Lock()
		m.logins[key] = next
		m.mu.Unlock()
	}()
	return state, nil
}
func (m *Manager) LoginStatus(ctx context.Context, owner, id string) (LoginState, error) {
	if _, err := m.connectionProvider(ctx, owner, id); err != nil {
		return LoginState{}, err
	}
	m.mu.Lock()
	state, ok := m.logins[owner+"/"+id]
	m.mu.Unlock()
	if ok {
		return state, nil
	}
	return LoginState{Status: "idle"}, nil
}
func (m *Manager) Models(ctx context.Context, owner, id string) ([]ModelOption, error) {
	p, err := m.connectionProvider(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if p.Kind == "codex" {
		c, err := m.codexClient(ctx)
		if err != nil {
			return nil, err
		}
		defer c.close()
		var account struct {
			Account *struct {
				Type string `json:"type"`
			} `json:"account"`
		}
		if err = c.call("account/read", map[string]any{"refreshToken": false}, &account); err != nil {
			return nil, err
		}
		if account.Account == nil || account.Account.Type != "chatgpt" {
			return nil, errors.New("codex_chatgpt_login_required_use_api_source_for_api_billing")
		}
		out := []ModelOption{}
		cursor := ""
		for {
			var page struct {
				Data []struct {
					Model  string `json:"model"`
					Name   string `json:"displayName"`
					Hidden bool   `json:"hidden"`
				} `json:"data"`
				Next *string `json:"nextCursor"`
			}
			params := map[string]any{"limit": 100}
			if cursor != "" {
				params["cursor"] = cursor
			}
			if err = c.call("model/list", params, &page); err != nil {
				return nil, err
			}
			for _, model := range page.Data {
				if !model.Hidden {
					out = append(out, ModelOption{model.Model, model.Name})
				}
			}
			if page.Next == nil || *page.Next == "" {
				return out, nil
			}
			if *page.Next == cursor || len(out) > 2000 {
				return nil, errors.New("model_catalog_pagination_failed")
			}
			cursor = *page.Next
		}
	}
	if p.Kind == "claude-cli" {
		return m.claudeModels(ctx)
	}
	if p.Kind == "source-extract" {
		return []ModelOption{}, nil
	}
	path, err := m.secretPath(owner, id)
	if err != nil {
		return nil, err
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("credential_not_configured")
	}
	endpoint := "https://api.openai.com/v1/models"
	if p.Kind == "anthropic" {
		endpoint = "https://api.anthropic.com/v1/models?limit=1000"
	}
	out := []ModelOption{}
	for {
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, err
		}
		if p.Kind == "anthropic" {
			req.Header.Set("x-api-key", strings.TrimSpace(string(key)))
			req.Header.Set("anthropic-version", "2023-06-01")
		} else {
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(key)))
		}
		res, err := m.HTTP.Do(req)
		if err != nil {
			return nil, errors.New("provider_connection_failed")
		}
		var page struct {
			Data []struct {
				ID   string `json:"id"`
				Name string `json:"display_name"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			Last    string `json:"last_id"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&page)
		res.Body.Close()
		if res.StatusCode != 200 {
			return nil, errors.New("provider_credentials_or_models_unavailable")
		}
		if err != nil {
			return nil, err
		}
		for _, model := range page.Data {
			name := model.Name
			if name == "" {
				name = model.ID
			}
			out = append(out, ModelOption{model.ID, name})
		}
		if p.Kind != "anthropic" || !page.HasMore {
			return out, nil
		}
		if page.Last == "" || len(out) > 2000 {
			return nil, errors.New("model_catalog_pagination_failed")
		}
		endpoint = "https://api.anthropic.com/v1/models?limit=1000&after_id=" + url.QueryEscape(page.Last)
	}
}
func (m *Manager) claudeModels(ctx context.Context) ([]ModelOption, error) {
	if err := ClaudeSubscription(ctx, m.Store.Root); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "claude", "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose")
	cmd.Dir = m.Store.Root
	cmd.Env = CLIEnvironment()
	cmd.Stderr = io.Discard
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		return nil, err
	}
	defer func() { input.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	err = json.NewEncoder(input).Encode(map[string]any{"type": "control_request", "request_id": "models", "request": map[string]string{"subtype": "initialize"}})
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(output)
	for {
		var msg struct {
			Type     string `json:"type"`
			Response struct {
				RequestID string `json:"request_id"`
				Response  struct {
					Models []struct {
						Value string `json:"value"`
						Name  string `json:"displayName"`
					} `json:"models"`
				} `json:"response"`
			} `json:"response"`
		}
		if dec.Decode(&msg) != nil {
			return nil, errors.New("claude_model_catalog_unavailable")
		}
		if msg.Type == "control_response" && msg.Response.RequestID == "models" {
			out := []ModelOption{}
			for _, model := range msg.Response.Response.Models {
				out = append(out, ModelOption{model.Value, model.Name})
			}
			if len(out) == 0 {
				return nil, errors.New("claude_model_catalog_unavailable")
			}
			return out, nil
		}
	}
}
