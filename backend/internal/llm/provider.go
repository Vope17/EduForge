package llm

import (
	"context"
	"encoding/json"
	"errors"
)

var ErrUnavailable = errors.New("generation provider is not configured")
var ErrInvalidOutput = errors.New("invalid model output")
var ErrPlanPermission = errors.New("ChatGPT plan permission not granted")
var ErrSubscriptionLimit = errors.New("ChatGPT plan usage limit reached")

type Request struct {
	Instruction string
	Data        json.RawMessage
	Schema      json.RawMessage
	MaxTokens   int
}

// Provider returns a single JSON value. Domain validation is done by the caller.
type Provider interface {
	Ready() bool
	Generate(ctx context.Context, request Request) (json.RawMessage, error)
}
