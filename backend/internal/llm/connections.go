package llm

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrConfiguration = errors.New("invalid model connection settings")
var ErrConnectionChanged = errors.New("model connection changed")

const loginTimeout = 15 * time.Minute

type ConnectionStatus struct {
	Connected bool
	Mode      string
	Source    string
	Endpoint  string
	Model     string
	Account   string
	Verified  bool
	Version   uint64
}

// LoginState is the ChatGPT device sign-in shown on the settings page.
// Status is idle, pending, connected or failed.
type LoginState struct {
	Status   string
	UserCode string
	URL      string
	Expires  time.Time
}

// Preferences keeps the chosen ChatGPT model across restarts ("" = Codex default).
type Preferences interface {
	ChatGPTModel(context.Context) (string, error)
	SaveChatGPTModel(context.Context, string) error
}

// Connections owns the active provider for the current single-user installation.
// API keys stay inside the provider; the ChatGPT sign-in stays in Codex CLI's
// files. Neither is ever part of ConnectionStatus.
type Connections struct {
	mu          sync.RWMutex
	provider    Provider
	status      ConnectionStatus
	codex       *Codex
	prefs       Preferences
	loginMu     sync.Mutex // serializes StartLogin
	login       LoginState
	loginSeq    uint64
	cancelLogin context.CancelFunc
}

// NewConnections starts with the API key from the environment, if any. codex
// and prefs may be nil; without codex, ChatGPT sign-in is unavailable.
func NewConnections(endpoint, model, key string, codex *Codex, prefs Preferences) (*Connections, error) {
	provider, err := NewChatCompletions(endpoint, model, key)
	if err != nil {
		return nil, err
	}
	manager := &Connections{codex: codex, prefs: prefs, login: LoginState{Status: "idle"}}
	if provider != nil {
		manager.provider = provider
		manager.status = ConnectionStatus{
			Connected: true, Mode: "api_key", Source: "environment",
			Endpoint: strings.TrimSpace(endpoint), Model: strings.TrimSpace(model), Version: 1,
		}
	}
	return manager, nil
}

// Restore reconnects to a ChatGPT sign-in Codex kept from an earlier run,
// unless another connection is already active.
func (m *Connections) Restore(ctx context.Context) error {
	if m.codex == nil {
		return nil
	}
	m.mu.RLock()
	connected, version := m.status.Connected, m.status.Version
	m.mu.RUnlock()
	if connected {
		return nil
	}
	account, err := m.codex.Account(ctx)
	if errors.Is(err, ErrUnavailable) {
		return nil
	}
	if err != nil {
		return err
	}
	model := ""
	if m.prefs != nil {
		if model, err = m.prefs.ChatGPTModel(ctx); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.Version != version {
		return nil
	}
	m.useChatGPT(account, model)
	return nil
}

// useChatGPT switches to the Codex provider. Callers hold m.mu.
func (m *Connections) useChatGPT(account, model string) {
	m.provider = &codexProvider{codex: m.codex, model: model}
	m.status = ConnectionStatus{
		Connected: true, Mode: "chatgpt", Source: "codex", Account: account, Model: model,
		Version: m.status.Version + 1,
	}
}

func (m *Connections) Status() ConnectionStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

// ChatGPTAvailable reports whether the Codex CLI is installed for sign-in.
func (m *Connections) ChatGPTAvailable() bool {
	return m.codex != nil && m.codex.Available()
}

// usable reports whether status names a model to use; a ChatGPT connection
// may leave the model to Codex's default.
func usable(provider Provider, status ConnectionStatus) bool {
	return provider != nil && (status.Model != "" || status.Mode == "chatgpt")
}

func (m *Connections) Ready() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return usable(m.provider, m.status)
}

func (m *Connections) SetAPIKey(endpoint, model, key string, version uint64) error {
	if len(endpoint) > 2048 || utf8.RuneCountInString(model) > 200 || len(key) > 8192 ||
		strings.TrimSpace(endpoint) == "" || strings.TrimSpace(model) == "" || strings.TrimSpace(key) == "" {
		return ErrConfiguration
	}
	provider, err := NewChatCompletions(endpoint, model, key)
	if err != nil {
		return ErrConfiguration
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.Version != version {
		return ErrConnectionChanged
	}
	m.provider = provider
	m.status = ConnectionStatus{
		Connected: true, Mode: "api_key", Source: "session",
		Endpoint: strings.TrimSpace(endpoint), Model: strings.TrimSpace(model), Version: version + 1,
	}
	return nil
}

func (m *Connections) Generate(ctx context.Context, request Request) (json.RawMessage, error) {
	m.mu.RLock()
	provider, status := m.provider, m.status
	m.mu.RUnlock()
	if !usable(provider, status) {
		return nil, ErrUnavailable
	}
	return provider.Generate(ctx, request)
}

func (m *Connections) Test(ctx context.Context, version uint64) error {
	m.mu.RLock()
	provider, current := m.provider, m.status
	m.mu.RUnlock()
	if !usable(provider, current) {
		return ErrUnavailable
	}
	if current.Version != version {
		return ErrConnectionChanged
	}
	raw, err := provider.Generate(ctx, Request{
		Instruction: "Return only a JSON object with status equal to ok.",
		Data:        json.RawMessage(`{}`),
		Schema:      json.RawMessage(`{"type":"object","additionalProperties":false,"required":["status"],"properties":{"status":{"type":"string","enum":["ok"]}}}`),
		MaxTokens:   128,
	})
	if err != nil {
		return err
	}
	var output struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &output) != nil || output.Status != "ok" {
		return ErrInvalidOutput
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.Version != version {
		return ErrConnectionChanged
	}
	m.status.Verified = true
	return nil
}

// StartLogin begins a ChatGPT device-code sign-in through Codex, or returns
// the one already waiting. Finishing it replaces the current connection.
func (m *Connections) StartLogin() (LoginState, error) {
	if m.codex == nil {
		return LoginState{}, ErrUnavailable
	}
	m.loginMu.Lock()
	defer m.loginMu.Unlock()
	if state := m.LoginStatus(); state.Status == "pending" {
		return state, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	login, err := m.codex.StartLogin(ctx)
	if err != nil {
		cancel()
		return LoginState{}, err
	}
	state := LoginState{Status: "pending", UserCode: login.UserCode, URL: login.URL, Expires: time.Now().Add(loginTimeout)}
	m.mu.Lock()
	m.loginSeq++
	seq := m.loginSeq
	m.login, m.cancelLogin = state, cancel
	m.mu.Unlock()
	go func() {
		result := <-login.Done
		cancel()
		if result.Err == nil && m.prefs != nil {
			// A new sign-in may be another account: start from Codex's default model.
			saveCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
			result.Err = m.prefs.SaveChatGPTModel(saveCtx, "")
			done()
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if seq != m.loginSeq {
			return
		}
		m.cancelLogin = nil
		if result.Err != nil {
			m.login = LoginState{Status: "failed"}
			return
		}
		m.useChatGPT(result.Account, "")
		m.login = LoginState{Status: "connected"}
	}()
	return state, nil
}

func (m *Connections) LoginStatus() LoginState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.login
}

// stopLogin abandons a waiting sign-in. Callers hold m.mu.
func (m *Connections) stopLogin() {
	if m.cancelLogin != nil {
		m.cancelLogin()
		m.cancelLogin = nil
	}
	m.loginSeq++
	m.login = LoginState{Status: "idle"}
}

func (m *Connections) SelectModel(ctx context.Context, model string, version uint64) error {
	m.mu.RLock()
	mode, current := m.status.Mode, m.status.Version
	m.mu.RUnlock()
	if mode != "chatgpt" {
		return ErrUnavailable
	}
	if version != current {
		return ErrConnectionChanged
	}
	if model != "" {
		models, err := m.codex.Models(ctx)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(models, func(item Model) bool { return item.ID == model }) {
			return ErrConfiguration
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.Version != version {
		return ErrConnectionChanged
	}
	if m.prefs != nil {
		if err := m.prefs.SaveChatGPTModel(ctx, model); err != nil {
			return err
		}
	}
	m.provider = &codexProvider{codex: m.codex, model: model}
	m.status.Model = model
	m.status.Verified = false
	m.status.Version++
	return nil
}

// Disconnect clears the connection. For ChatGPT it also signs Codex out, and
// reports false when Codex could not remove its stored sign-in.
func (m *Connections) Disconnect(ctx context.Context, version uint64) (bool, error) {
	m.mu.Lock()
	if m.status.Version != version {
		m.mu.Unlock()
		return false, ErrConnectionChanged
	}
	mode := m.status.Mode
	m.provider = nil
	m.status = ConnectionStatus{Version: version + 1}
	m.stopLogin()
	m.mu.Unlock()
	if mode != "chatgpt" {
		return true, nil
	}
	loggedOut := m.codex.Logout(ctx) == nil
	if m.prefs != nil {
		_ = m.prefs.SaveChatGPTModel(ctx, "")
	}
	return loggedOut, nil
}

func (m *Connections) Models(ctx context.Context) ([]Model, error) {
	m.mu.RLock()
	mode := m.status.Mode
	m.mu.RUnlock()
	if mode != "chatgpt" {
		return nil, ErrUnavailable
	}
	return m.codex.Models(ctx)
}
