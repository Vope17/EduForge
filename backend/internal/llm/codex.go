package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Model is a model the signed-in ChatGPT account may use.
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ErrCodex reports a Codex CLI failure that is not a sign-in or quota problem.
var ErrCodex = errors.New("Codex CLI request failed")

const codexInstructions = "You write teaching content for EduForge. Reply with the requested JSON only. " +
	"Never run commands, read or write files, browse, or call tools."

// Codex talks to the official Codex CLI (`codex app-server`) over stdio. The
// ChatGPT sign-in lives in Codex's own files under CODEX_HOME; EduForge never
// reads or stores the tokens. Each call starts a short-lived app-server.
type Codex struct {
	// Command is the executable followed by leading arguments; nil means "codex".
	Command []string
}

// Available reports whether the Codex executable can be found.
func (c *Codex) Available() bool {
	_, err := exec.LookPath(c.command()[0])
	return err == nil
}

func (c *Codex) command() []string {
	if len(c.Command) == 0 {
		return []string{"codex"}
	}
	return c.Command
}

// codexEnvironment passes only what Codex needs, so EduForge's own secrets
// (such as EDUFORGE_LLM_API_KEY) never reach the child process.
func codexEnvironment() []string {
	keep := []string{"PATH", "HOME", "CODEX_HOME", "TMPDIR", "LANG", "LC_ALL", "SSL_CERT_FILE", "SSL_CERT_DIR",
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy"}
	env := make([]string, 0, len(keep))
	for _, name := range keep {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

type codexSession struct {
	cmd     *exec.Cmd
	input   io.WriteCloser
	output  *bufio.Scanner
	encoder *json.Encoder
	next    int
	cwd     string
}

type codexMessage struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

// start launches app-server with tools, apps, plugins, MCP and web search off,
// in an empty working directory, and completes the initialize handshake.
func (c *Codex) start(ctx context.Context) (*codexSession, error) {
	cwd, err := os.MkdirTemp("", "eduforge-codex-")
	if err != nil {
		return nil, fmt.Errorf("create Codex working directory: %w", err)
	}
	command := c.command()
	args := append(append([]string{}, command[1:]...), "app-server", "--listen", "stdio://",
		"--disable", "shell_tool", "--disable", "apps", "--disable", "plugins",
		"-c", `web_search="disabled"`, "-c", "mcp_servers={}")
	cmd := exec.CommandContext(ctx, command[0], args...)
	cmd.Dir = cwd
	cmd.Env = codexEnvironment()
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	input, err := cmd.StdinPipe()
	if err == nil {
		var output io.ReadCloser
		output, err = cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err == nil {
			scanner := bufio.NewScanner(output)
			scanner.Buffer(make([]byte, 64<<10), 8<<20)
			session := &codexSession{cmd: cmd, input: input, output: scanner, encoder: json.NewEncoder(input), cwd: cwd}
			if err := session.call("initialize", map[string]any{
				"clientInfo": map[string]string{"name": "eduforge", "title": "EduForge", "version": "0.1.0"},
			}, nil, nil); err != nil {
				session.close()
				return nil, err
			}
			if err := session.encoder.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
				session.close()
				return nil, ErrUnavailable
			}
			return session, nil
		}
	}
	os.RemoveAll(cwd)
	return nil, fmt.Errorf("%w: Codex CLI could not be started", ErrUnavailable)
}

// close ends stdin so app-server can exit on its own, and kills it if it has
// not exited within two seconds.
func (s *codexSession) close() {
	s.input.Close()
	exited := make(chan struct{})
	go func() {
		_ = s.cmd.Wait()
		close(exited)
	}()
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		_ = s.cmd.Process.Kill()
		<-exited
	}
	os.RemoveAll(s.cwd)
}

// read returns the next message. Requests from the server (approvals, tool
// calls) are refused, since content generation never needs them.
func (s *codexSession) read() (codexMessage, error) {
	for s.output.Scan() {
		var message codexMessage
		if json.Unmarshal(s.output.Bytes(), &message) != nil {
			continue
		}
		if message.ID != nil && message.Method != "" {
			_ = s.encoder.Encode(map[string]any{"id": *message.ID, "error": map[string]any{
				"code": -32601, "message": "EduForge does not allow tools or approvals",
			}})
			continue
		}
		return message, nil
	}
	return codexMessage{}, fmt.Errorf("%w: Codex app-server closed the connection", ErrCodex)
}

// call sends a request and waits for its response. Notifications that arrive
// meanwhile go to notify when it is set.
func (s *codexSession) call(method string, params, result any, notify func(codexMessage)) error {
	s.next++
	id := s.next
	if err := s.encoder.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return fmt.Errorf("%w: write %s", ErrCodex, method)
	}
	for {
		message, err := s.read()
		if err != nil {
			return err
		}
		if message.ID == nil {
			if notify != nil {
				notify(message)
			}
			continue
		}
		if *message.ID != id {
			continue
		}
		if len(message.Error) > 0 && string(message.Error) != "null" {
			return fmt.Errorf("%w: %s was rejected", ErrCodex, method)
		}
		if result != nil && json.Unmarshal(message.Result, result) != nil {
			return fmt.Errorf("%w: invalid %s response", ErrCodex, method)
		}
		return nil
	}
}

// account returns the signed-in ChatGPT email, or ErrUnavailable when Codex
// has no ChatGPT sign-in (an API-key login does not count).
func (s *codexSession) account() (string, error) {
	var result struct {
		Account *struct {
			Type  string  `json:"type"`
			Email *string `json:"email"`
		} `json:"account"`
	}
	if err := s.call("account/read", map[string]any{"refreshToken": false}, &result, nil); err != nil {
		return "", err
	}
	if result.Account == nil || result.Account.Type != "chatgpt" {
		return "", ErrUnavailable
	}
	if result.Account.Email == nil {
		return "", nil
	}
	return *result.Account.Email, nil
}

// Account returns the email of the ChatGPT account Codex is signed in to.
func (c *Codex) Account(ctx context.Context) (string, error) {
	session, err := c.start(ctx)
	if err != nil {
		return "", err
	}
	defer session.close()
	return session.account()
}

// Models lists the models offered to the signed-in account.
func (c *Codex) Models(ctx context.Context) ([]Model, error) {
	session, err := c.start(ctx)
	if err != nil {
		return nil, err
	}
	defer session.close()
	if _, err := session.account(); err != nil {
		return nil, err
	}
	models := make([]Model, 0)
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
		if err := session.call("model/list", params, &page, nil); err != nil {
			return nil, err
		}
		for _, model := range page.Data {
			if !model.Hidden && model.Model != "" {
				models = append(models, Model{ID: model.Model, Name: model.Name})
			}
		}
		if page.Next == nil || *page.Next == "" {
			return models, nil
		}
		if *page.Next == cursor || len(models) > 2000 {
			return nil, fmt.Errorf("%w: model list did not end", ErrCodex)
		}
		cursor = *page.Next
	}
}

// Logout removes Codex's stored ChatGPT sign-in.
func (c *Codex) Logout(ctx context.Context) error {
	session, err := c.start(ctx)
	if err != nil {
		return err
	}
	defer session.close()
	return session.call("account/logout", nil, nil, nil)
}

// DeviceLogin is a ChatGPT device-code sign-in in progress.
type DeviceLogin struct {
	UserCode string
	URL      string
	// Done receives the sign-in result once: the account email, or an error.
	Done <-chan LoginResult
}

type LoginResult struct {
	Account string
	Err     error
}

// StartLogin begins a device-code sign-in. The app-server keeps running until
// the user finishes in the browser, ctx ends, or the code expires.
func (c *Codex) StartLogin(ctx context.Context) (DeviceLogin, error) {
	session, err := c.start(ctx)
	if err != nil {
		return DeviceLogin{}, err
	}
	var started struct {
		UserCode string `json:"userCode"`
		URL      string `json:"verificationUrl"`
	}
	if err := session.call("account/login/start", map[string]string{"type": "chatgptDeviceCode"}, &started, nil); err != nil {
		session.close()
		return DeviceLogin{}, err
	}
	target, err := url.Parse(started.URL)
	if err != nil || target.Scheme != "https" || target.Hostname() != "auth.openai.com" || started.UserCode == "" {
		session.close()
		return DeviceLogin{}, fmt.Errorf("%w: unexpected sign-in address", ErrCodex)
	}
	done := make(chan LoginResult, 1)
	go func() {
		defer session.close()
		for {
			message, err := session.read()
			if err != nil {
				done <- LoginResult{Err: errors.Join(ErrCodex, ctx.Err())}
				return
			}
			if message.Method != "account/login/completed" {
				continue
			}
			var completed struct {
				Success bool `json:"success"`
			}
			if json.Unmarshal(message.Params, &completed) != nil || !completed.Success {
				done <- LoginResult{Err: fmt.Errorf("%w: sign-in was not completed", ErrCodex)}
				return
			}
			account, err := session.account()
			done <- LoginResult{Account: account, Err: err}
			return
		}
	}()
	return DeviceLogin{UserCode: started.UserCode, URL: target.String(), Done: done}, nil
}

// codexTurnError maps a failed turn to the errors EduForge can explain.
func codexTurnError(raw json.RawMessage) error {
	var turnError struct {
		Info json.RawMessage `json:"codexErrorInfo"`
	}
	_ = json.Unmarshal(raw, &turnError)
	var info string
	_ = json.Unmarshal(turnError.Info, &info)
	switch info {
	case "usageLimitExceeded", "rateLimitExceeded":
		return ErrSubscriptionLimit
	case "unauthorized":
		return ErrUnavailable
	}
	return fmt.Errorf("%w: turn failed", ErrCodex)
}

// Generate runs one ephemeral turn whose final message must match input.Schema.
// MaxTokens is not supported by Codex and is ignored.
func (c *Codex) Generate(ctx context.Context, model string, input Request) (json.RawMessage, error) {
	session, err := c.start(ctx)
	if err != nil {
		return nil, err
	}
	defer session.close()
	if _, err := session.account(); err != nil {
		return nil, err
	}
	thread := map[string]any{
		"cwd": session.cwd, "approvalPolicy": "never", "sandbox": "read-only", "ephemeral": true,
		"developerInstructions": codexInstructions + "\n\n" + input.Instruction,
	}
	if model != "" {
		thread["model"] = model
	}
	var started struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := session.call("thread/start", thread, &started, nil); err != nil {
		return nil, err
	}
	var final string
	var turnErr error
	finished := false
	collect := func(message codexMessage) {
		switch message.Method {
		case "item/completed":
			var event struct {
				Item struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"item"`
			}
			if json.Unmarshal(message.Params, &event) == nil && event.Item.Type == "agentMessage" {
				final = event.Item.Text
			}
		case "turn/completed":
			var event struct {
				Turn struct {
					Status string          `json:"status"`
					Error  json.RawMessage `json:"error"`
				} `json:"turn"`
			}
			finished = true
			if json.Unmarshal(message.Params, &event) != nil {
				turnErr = fmt.Errorf("%w: invalid turn result", ErrCodex)
			} else if event.Turn.Status != "completed" {
				turnErr = codexTurnError(event.Turn.Error)
			}
		}
	}
	if err := session.call("turn/start", map[string]any{
		"threadId":     started.Thread.ID,
		"input":        []any{map[string]any{"type": "text", "text": string(input.Data), "text_elements": []any{}}},
		"outputSchema": input.Schema,
	}, nil, collect); err != nil {
		return nil, err
	}
	for !finished {
		message, err := session.read()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		if message.ID == nil {
			collect(message)
		}
	}
	if turnErr != nil {
		return nil, turnErr
	}
	final = strings.TrimSpace(final)
	if final == "" || !json.Valid([]byte(final)) {
		return nil, ErrInvalidOutput
	}
	return json.RawMessage(final), nil
}

// codexProvider generates with a fixed model ("" lets Codex choose its default).
type codexProvider struct {
	codex *Codex
	model string
}

func (p *codexProvider) Ready() bool { return true }

func (p *codexProvider) Generate(ctx context.Context, input Request) (json.RawMessage, error) {
	return p.codex.Generate(ctx, p.model, input)
}
