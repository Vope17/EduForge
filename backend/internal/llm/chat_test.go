package llm

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestChatCompletionsUsesConfiguredService(t *testing.T) {
	provider, err := NewChatCompletions("https://provider.example/v1/chat/completions", "test-model", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	client := provider.(*chatProvider)
	client.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "provider.example" || r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			t.Fatal("incorrect request destination or method")
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatal("missing authorization header")
		}
		var body struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_completion_tokens"`
			Format    struct {
				Type   string `json:"type"`
				Schema struct {
					Strict bool `json:"strict"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "test-model" || body.MaxTokens != 1200 || body.Format.Type != "json_schema" || !body.Format.Schema.Strict {
			t.Fatalf("unexpected chat request: %+v", body)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				`{"choices":[{"finish_reason":"stop","message":{"content":"{\"title\":\"Example\"}"}}]}`,
			)),
		}, nil
	})
	result, err := provider.Generate(t.Context(), Request{
		Instruction: "Return JSON.",
		Data:        json.RawMessage(`{"topic":"Water"}`),
		Schema:      json.RawMessage(`{"type":"object"}`),
		MaxTokens:   1200,
	})
	if err != nil || string(result) != `{"title":"Example"}` {
		t.Fatalf("response = %q, error = %v", result, err)
	}
}
