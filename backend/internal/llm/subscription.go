package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type subscriptionProvider struct {
	mu       sync.Mutex
	oauth    *OAuth
	clientID string
	tokens   tokenSet
	expires  time.Time
	model    string
}

func (p *subscriptionProvider) Ready() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.model != ""
}

func (p *subscriptionProvider) setModel(model string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.model = model
}

func (p *subscriptionProvider) accessToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Now().Add(time.Minute).Before(p.expires) {
		return p.tokens.AccessToken, nil
	}
	if p.tokens.RefreshToken == "" {
		return "", ErrUnavailable
	}
	config, err := p.oauth.loadDiscovery(ctx)
	if err != nil {
		return "", err
	}
	next, err := p.oauth.exchange(ctx, config.Token, url.Values{
		"grant_type": {"refresh_token"}, "client_id": {p.clientID},
		"refresh_token": {p.tokens.RefreshToken}, "resource": {responsesResource},
	})
	if err != nil {
		return "", ErrUnavailable
	}
	if next.Scope != "" && (!hasPlanScopes(next.Scope)) {
		return "", ErrPlanPermission
	}
	if next.RefreshToken == "" {
		next.RefreshToken = p.tokens.RefreshToken
	}
	if next.Scope == "" {
		next.Scope = p.tokens.Scope
	}
	p.tokens = next
	p.expires = time.Now().Add(time.Duration(next.ExpiresIn) * time.Second)
	return p.tokens.AccessToken, nil
}

func hasPlanScopes(scope string) bool {
	fields := " " + strings.Join(strings.Fields(scope), " ") + " "
	return strings.Contains(fields, " chatgpt.tokens.use.direct ") && strings.Contains(fields, " resource.invoke ")
}

func (p *subscriptionProvider) Models(ctx context.Context) ([]Model, error) {
	token, err := p.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, responsesResource+"/models", nil)
	if err != nil {
		return nil, ErrAuthorization
	}
	request.Header.Set("Authorization", "Bearer "+token)
	var catalog struct {
		Models []struct {
			Slug       string `json:"slug"`
			Name       string `json:"display_name"`
			Visibility string `json:"visibility"`
		} `json:"models"`
	}
	if err := p.oauth.requestJSON(request, &catalog); err != nil {
		return nil, err
	}
	models := make([]Model, 0)
	for _, model := range catalog.Models {
		if model.Visibility == "list" && model.Slug != "" {
			models = append(models, Model{ID: model.Slug, Name: model.Name})
		}
	}
	return models, nil
}

func (p *subscriptionProvider) Generate(ctx context.Context, input Request) (_ json.RawMessage, err error) {
	token, err := p.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	model := p.model
	p.mu.Unlock()
	if model == "" {
		return nil, ErrUnavailable
	}
	data, err := json.Marshal(map[string]any{
		"model": model, "store": false, "stream": true, "max_output_tokens": input.MaxTokens,
		"input": []map[string]string{
			{"role": "system", "content": input.Instruction}, {"role": "user", "content": string(input.Data)},
		},
		"text": map[string]any{"format": map[string]any{
			"type": "json_schema", "name": "teaching_content", "strict": true, "schema": input.Schema,
		}},
	})
	if err != nil {
		return nil, ErrInvalidOutput
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, responsesEndpoint, bytes.NewReader(data))
	if err != nil {
		return nil, ErrAuthorization
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	client := *p.oauth.client
	client.Timeout = 3 * time.Minute
	response, err := client.Do(request)
	if err != nil {
		return nil, ErrAuthorization
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil && err == nil {
			err = ErrAuthorization
		}
	}()
	if response.StatusCode == http.StatusTooManyRequests {
		return nil, ErrSubscriptionLimit
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, ErrUnavailable
	}
	if response.StatusCode != http.StatusOK {
		return nil, ErrAuthorization
	}
	return completedResponse(response.Body)
}

func completedResponse(body io.Reader) (json.RawMessage, error) {
	scanner := bufio.NewScanner(io.LimitReader(body, 2<<20))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var data strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			data.WriteByte('\n')
			continue
		}
		if line != "" || data.Len() == 0 {
			continue
		}
		var event struct {
			Type     string `json:"type"`
			Response struct {
				Status string `json:"status"`
				Error  struct {
					Code string `json:"code"`
				} `json:"error"`
				Output []struct {
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"output"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(data.String()), &event) != nil {
			return nil, ErrInvalidOutput
		}
		data.Reset()
		switch event.Type {
		case "response.completed":
			if event.Response.Status != "completed" {
				return nil, ErrInvalidOutput
			}
			var result strings.Builder
			for _, item := range event.Response.Output {
				for _, content := range item.Content {
					if content.Type == "output_text" {
						result.WriteString(content.Text)
					}
				}
			}
			if result.Len() == 0 {
				return nil, ErrInvalidOutput
			}
			return json.RawMessage(result.String()), nil
		case "response.failed":
			if event.Response.Error.Code == "subscription_sharing_usage_limit_exceeded" {
				return nil, ErrSubscriptionLimit
			}
			if event.Response.Error.Code == "subscription_sharing_usage_unavailable" {
				return nil, ErrPlanPermission
			}
			return nil, ErrAuthorization
		case "response.incomplete", "error":
			return nil, ErrInvalidOutput
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.Join(ErrInvalidOutput, err)
	}
	return nil, ErrInvalidOutput
}

func (p *subscriptionProvider) Revoke(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	defer func() {
		p.tokens = tokenSet{}
		p.expires = time.Time{}
		p.model = ""
	}()
	if p.tokens.RefreshToken == "" {
		return nil
	}
	config, err := p.oauth.loadDiscovery(ctx)
	if err != nil || config.Revocation == "" {
		return ErrAuthorization
	}
	form := url.Values{"token": {p.tokens.RefreshToken}, "token_type_hint": {"refresh_token"}, "client_id": {p.clientID}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, config.Revocation, strings.NewReader(form.Encode()))
	if err != nil {
		return ErrAuthorization
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := p.oauth.client.Do(request)
	if err != nil {
		return ErrAuthorization
	}
	closeErr := response.Body.Close()
	if response.StatusCode != http.StatusOK || closeErr != nil {
		return ErrAuthorization
	}
	return nil
}
