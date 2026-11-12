package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type chatProvider struct {
	endpoint string
	model    string
	key      string
	client   *http.Client
}

func NewChatCompletions(endpoint, model, key string) (Provider, error) {
	endpoint = strings.TrimSpace(endpoint)
	model = strings.TrimSpace(model)
	key = strings.TrimSpace(key)
	if endpoint == "" && model == "" && key == "" {
		return nil, nil
	}
	if endpoint == "" || model == "" || key == "" {
		return nil, errors.New("set EDUFORGE_LLM_ENDPOINT, EDUFORGE_LLM_MODEL and EDUFORGE_LLM_API_KEY together")
	}
	target, err := url.Parse(endpoint)
	if err != nil || target.Scheme != "https" || target.Hostname() == "" || target.User != nil ||
		target.ForceQuery || target.RawQuery != "" || target.Fragment != "" {
		return nil, errors.New("EDUFORGE_LLM_ENDPOINT must be an HTTPS URL without credentials, query or fragment")
	}
	return &chatProvider{
		endpoint: target.String(),
		model:    model,
		key:      key,
		client: &http.Client{
			Timeout: 3 * time.Minute,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (p *chatProvider) Ready() bool { return true }

func (p *chatProvider) Generate(ctx context.Context, input Request) (_ json.RawMessage, err error) {
	body, err := json.Marshal(map[string]any{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "system", "content": input.Instruction},
			{"role": "user", "content": string(input.Data)},
		},
		"max_completion_tokens": input.MaxTokens,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name": "teaching_content", "strict": true, "schema": input.Schema,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode chat request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("construct chat request")
	}
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, errors.New("send chat request")
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			err = errors.New("close chat response")
		}
	}()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chat request returned status %d", resp.StatusCode)
	}

	const maxResponse = 1 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, errors.New("read chat response")
	}
	if len(data) > maxResponse {
		return nil, ErrInvalidOutput
	}
	var result struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &result); err != nil || len(result.Choices) != 1 {
		return nil, ErrInvalidOutput
	}
	choice := result.Choices[0]
	if choice.FinishReason != "stop" || choice.Message.Refusal != "" || choice.Message.Content == "" {
		return nil, ErrInvalidOutput
	}
	return json.RawMessage(choice.Message.Content), nil
}
