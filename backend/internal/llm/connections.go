package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"unicode/utf8"
)

var ErrConfiguration = errors.New("invalid model connection settings")
var ErrConnectionChanged = errors.New("model connection changed")

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

// Connections owns the active provider for the current single-user installation.
// Credentials remain inside the provider and are never part of ConnectionStatus.
type Connections struct {
	mu       sync.RWMutex
	provider Provider
	status   ConnectionStatus
}

func NewConnections(endpoint, model, key string) (*Connections, error) {
	provider, err := NewChatCompletions(endpoint, model, key)
	if err != nil {
		return nil, err
	}
	manager := &Connections{}
	if provider != nil {
		manager.provider = provider
		manager.status = ConnectionStatus{
			Connected: true, Mode: "api_key", Source: "environment",
			Endpoint: strings.TrimSpace(endpoint), Model: strings.TrimSpace(model), Version: 1,
		}
	}
	return manager, nil
}

func (m *Connections) Status() ConnectionStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *Connections) Ready() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.provider != nil && m.status.Model != ""
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
	provider := m.provider
	model := m.status.Model
	m.mu.RUnlock()
	if provider == nil || model == "" {
		return nil, ErrUnavailable
	}
	return provider.Generate(ctx, request)
}

func (m *Connections) Test(ctx context.Context, version uint64) error {
	m.mu.RLock()
	provider := m.provider
	current := m.status
	m.mu.RUnlock()
	if provider == nil || current.Model == "" {
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

func (m *Connections) installChatGPT(provider *subscriptionProvider, account string, version uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.Version != version {
		return ErrConnectionChanged
	}
	m.provider = provider
	m.status = ConnectionStatus{
		Connected: true, Mode: "chatgpt", Source: "session", Account: account,
		Endpoint: responsesEndpoint, Version: version + 1,
	}
	return nil
}

func (m *Connections) subscription() (*subscriptionProvider, uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	provider, ok := m.provider.(*subscriptionProvider)
	if !ok {
		return nil, m.status.Version, ErrUnavailable
	}
	return provider, m.status.Version, nil
}

func (m *Connections) SelectModel(ctx context.Context, model string, version uint64) error {
	provider, current, err := m.subscription()
	if err != nil {
		return err
	}
	if version != current {
		return ErrConnectionChanged
	}
	models, err := provider.Models(ctx)
	if err != nil {
		return err
	}
	for _, item := range models {
		if item.ID != model {
			continue
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.status.Version != version {
			return ErrConnectionChanged
		}
		provider.setModel(model)
		m.status.Model = model
		m.status.Verified = false
		m.status.Version++
		return nil
	}
	return ErrConfiguration
}

// Disconnect clears the local session even when remote revocation fails.
func (m *Connections) Disconnect(ctx context.Context, version uint64) (bool, error) {
	m.mu.Lock()
	if m.status.Version != version {
		m.mu.Unlock()
		return false, ErrConnectionChanged
	}
	provider := m.provider
	m.provider = nil
	m.status = ConnectionStatus{Version: version + 1}
	m.mu.Unlock()
	if subscription, ok := provider.(*subscriptionProvider); ok {
		return subscription.Revoke(ctx) == nil, nil
	}
	return true, nil
}

func (m *Connections) Models(ctx context.Context) ([]Model, error) {
	provider, _, err := m.subscription()
	if err != nil {
		return nil, err
	}
	return provider.Models(ctx)
}
