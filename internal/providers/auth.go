package providers

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"

	"eduforge/internal/domain"
)

func (m *Manager) VerifyCLI(ctx context.Context, owner, id string) error {
	row, err := m.Store.Get(ctx, owner, id, "provider")
	if err != nil {
		return err
	}
	p, err := domain.Decode[domain.Provider](row.Data)
	if err != nil {
		return err
	}
	if p.Kind != "codex" && p.Kind != "claude-cli" {
		return errors.New("subscription_cli_required")
	}
	text, usage, err := m.call(ctx, owner, id, p, `Reply with exactly this JSON, without tools or commentary: {"title":"EduForge","body":"ready","citations":[]}`)
	if err != nil {
		return err
	}
	var section domain.Section
	if json.Unmarshal([]byte(strings.TrimSpace(text)), &section) != nil || section.Title != "EduForge" || section.Body != "ready" {
		return errors.New("cli_test_unexpected_reply")
	}
	model := usage.Model
	if model == "" {
		model = p.Model
	}
	m.Store.Event(ctx, owner, "", "", "provider_inference_verified", map[string]string{"provider_id": id, "kind": p.Kind, "model": model})
	return nil
}

// Subscription sources must not silently use a stored Console/API credential.
func ClaudeSubscription(ctx context.Context, cwd string) error {
	cmd := exec.CommandContext(ctx, "claude", "auth", "status")
	cmd.Dir = cwd
	cmd.Env = CLIEnvironment()
	out, err := cmd.Output()
	if err != nil {
		return errors.New("claude_subscription_login_required")
	}
	var status struct {
		LoggedIn bool   `json:"loggedIn"`
		Method   string `json:"authMethod"`
	}
	if json.Unmarshal(out, &status) != nil || !status.LoggedIn || (status.Method != "claude.ai" && status.Method != "oauth_token") {
		return errors.New("claude_subscription_auth_required_use_api_source_for_console_billing")
	}
	return nil
}
