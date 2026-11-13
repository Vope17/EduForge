package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as a fake `codex app-server` (see TestFakeCodexAppServer).
// Like the real CLI it keeps its sign-in in CODEX_HOME: auth.json exists while
// signed in. scenario.json sets the replies; requests.jsonl records each request.
type fakeScenario struct {
	Output    string          `json:"output"`
	TurnError json.RawMessage `json:"turn_error,omitempty"`
	LoginFail bool            `json:"login_fail"`
}

func fakeCodex(t *testing.T, scenario fakeScenario, signedIn bool) (*Codex, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	t.Setenv("EDUFORGE_LLM_API_KEY", "must-not-reach-codex")
	data, _ := json.Marshal(scenario)
	if err := os.WriteFile(filepath.Join(home, "scenario.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if signedIn {
		if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return &Codex{Command: []string{os.Args[0], "-test.run=^TestFakeCodexAppServer$", "--"}}, home
}

type fakeRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func fakeRequests(t *testing.T, home string) []fakeRequest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "requests.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var requests []fakeRequest
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var request fakeRequest
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
	}
	return requests
}

func findRequest(requests []fakeRequest, method string) (fakeRequest, bool) {
	index := slices.IndexFunc(requests, func(r fakeRequest) bool { return r.Method == method })
	if index < 0 {
		return fakeRequest{}, false
	}
	return requests[index], true
}

// TestFakeCodexAppServer is only active when started by a test as a child process.
func TestFakeCodexAppServer(t *testing.T) {
	if !slices.Contains(os.Args, "app-server") {
		return
	}
	home := os.Getenv("CODEX_HOME")
	var scenario fakeScenario
	data, _ := os.ReadFile(filepath.Join(home, "scenario.json"))
	_ = json.Unmarshal(data, &scenario)
	log, _ := os.OpenFile(filepath.Join(home, "requests.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	out := json.NewEncoder(os.Stdout)
	auth := filepath.Join(home, "auth.json")
	reply := func(id json.RawMessage, result any) { _ = out.Encode(map[string]any{"id": id, "result": result}) }
	notify := func(method string, params any) { _ = out.Encode(map[string]any{"method": method, "params": params}) }
	input := bufio.NewScanner(os.Stdin)
	input.Buffer(make([]byte, 64<<10), 8<<20)
	for input.Scan() {
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(input.Bytes(), &message) != nil {
			continue
		}
		if message.Method != "" {
			line, _ := json.Marshal(map[string]any{"method": message.Method, "params": message.Params})
			_, _ = log.Write(append(line, '\n'))
		}
		switch message.Method {
		case "initialize":
			reply(message.ID, map[string]any{})
		case "account/read":
			if _, err := os.Stat(auth); err != nil {
				reply(message.ID, map[string]any{"account": nil, "requiresOpenaiAuth": true})
			} else {
				reply(message.ID, map[string]any{"account": map[string]any{
					"type": "chatgpt", "email": "teacher@example.com", "planType": "plus",
				}})
			}
		case "account/logout":
			_ = os.Remove(auth)
			reply(message.ID, map[string]any{})
		case "account/login/start":
			reply(message.ID, map[string]any{"type": "chatgptDeviceCode", "loginId": "login-1",
				"verificationUrl": "https://auth.openai.com/codex/device", "userCode": "ABCD-1234"})
			if !scenario.LoginFail {
				_ = os.WriteFile(auth, []byte(`{}`), 0o600)
			}
			notify("account/login/completed", map[string]any{"loginId": "login-1", "success": !scenario.LoginFail})
		case "model/list":
			reply(message.ID, map[string]any{"data": []map[string]any{
				{"model": "gpt-fixture", "displayName": "Fixture", "hidden": false},
				{"model": "gpt-hidden", "displayName": "Hidden", "hidden": true},
			}, "nextCursor": nil})
		case "thread/start":
			reply(message.ID, map[string]any{"thread": map[string]any{"id": "thread-1"}, "model": "gpt-fixture"})
		case "turn/start":
			reply(message.ID, map[string]any{"turn": map[string]any{"id": "turn-1", "status": "inProgress"}})
			// Tool requests must be refused; the reply is recorded by the next loop.
			_ = out.Encode(map[string]any{"id": 900, "method": "item/commandExecution/requestApproval", "params": map[string]any{}})
			notify("item/completed", map[string]any{"item": map[string]any{"type": "agentMessage", "text": "draft"}})
			notify("item/completed", map[string]any{"item": map[string]any{"type": "agentMessage", "text": scenario.Output}})
			status := "completed"
			var turnError any
			if len(scenario.TurnError) > 0 {
				status, turnError = "failed", scenario.TurnError
			}
			notify("turn/completed", map[string]any{"threadId": "thread-1",
				"turn": map[string]any{"id": "turn-1", "status": status, "error": turnError}})
		case "":
			if len(message.Error) > 0 {
				line, _ := json.Marshal(map[string]any{"method": "(refused)", "params": message.Error})
				_, _ = log.Write(append(line, '\n'))
			}
		}
	}
	os.Exit(0)
}

func TestCodexGenerateConstrainsOutputAndRefusesTools(t *testing.T) {
	codex, home := fakeCodex(t, fakeScenario{Output: `{"status":"ok"}`}, true)
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"required":["status"],"properties":{"status":{"type":"string"}}}`)
	raw, err := codex.Generate(t.Context(), "gpt-fixture", Request{
		Instruction: "Write a lesson.", Data: json.RawMessage(`{"topic":"水循環"}`), Schema: schema,
	})
	if err != nil || string(raw) != `{"status":"ok"}` {
		t.Fatalf("unexpected result %s, %v", raw, err)
	}
	requests := fakeRequests(t, home)
	thread, _ := findRequest(requests, "thread/start")
	var threadParams struct {
		Model        string `json:"model"`
		Approval     string `json:"approvalPolicy"`
		Sandbox      string `json:"sandbox"`
		Ephemeral    bool   `json:"ephemeral"`
		Instructions string `json:"developerInstructions"`
	}
	_ = json.Unmarshal(thread.Params, &threadParams)
	if threadParams.Model != "gpt-fixture" || threadParams.Approval != "never" || threadParams.Sandbox != "read-only" ||
		!threadParams.Ephemeral || !strings.HasSuffix(threadParams.Instructions, "Write a lesson.") {
		t.Fatalf("unsafe or incomplete thread settings: %s", thread.Params)
	}
	turn, _ := findRequest(requests, "turn/start")
	var turnParams struct {
		Input []struct {
			Text string `json:"text"`
		} `json:"input"`
		Schema json.RawMessage `json:"outputSchema"`
	}
	_ = json.Unmarshal(turn.Params, &turnParams)
	if len(turnParams.Input) != 1 || turnParams.Input[0].Text != `{"topic":"水循環"}` || string(turnParams.Schema) != string(schema) {
		t.Fatalf("turn did not carry data and schema: %s", turn.Params)
	}
	if _, refused := findRequest(requests, "(refused)"); !refused {
		t.Fatal("tool approval request was not refused")
	}
}

func TestCodexEnvironmentExcludesEduForgeSecrets(t *testing.T) {
	fakeCodex(t, fakeScenario{}, false)
	for _, entry := range codexEnvironment() {
		if strings.Contains(entry, "must-not-reach-codex") {
			t.Fatal("API key was passed to Codex")
		}
	}
}

func TestCodexErrorsStayActionable(t *testing.T) {
	limit := json.RawMessage(`{"message":"limit","codexErrorInfo":"usageLimitExceeded"}`)
	codex, _ := fakeCodex(t, fakeScenario{TurnError: limit}, true)
	if _, err := codex.Generate(t.Context(), "", Request{Schema: json.RawMessage(`{}`)}); !errors.Is(err, ErrSubscriptionLimit) {
		t.Fatalf("usage limit was hidden: %v", err)
	}
	codex, _ = fakeCodex(t, fakeScenario{Output: "not json"}, true)
	if _, err := codex.Generate(t.Context(), "", Request{Schema: json.RawMessage(`{}`)}); !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("invalid output accepted: %v", err)
	}
	codex, _ = fakeCodex(t, fakeScenario{Output: `{}`}, false)
	if _, err := codex.Generate(t.Context(), "", Request{Schema: json.RawMessage(`{}`)}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("generated without a ChatGPT sign-in: %v", err)
	}
	missing := &Codex{Command: []string{filepath.Join(t.TempDir(), "codex")}}
	if _, err := missing.Account(t.Context()); !errors.Is(err, ErrUnavailable) || missing.Available() {
		t.Fatalf("missing CLI not reported as unavailable: %v", err)
	}
}

type memoryPreferences struct{ model string }

func (p *memoryPreferences) ChatGPTModel(context.Context) (string, error) { return p.model, nil }
func (p *memoryPreferences) SaveChatGPTModel(_ context.Context, model string) error {
	p.model = model
	return nil
}

func waitForLogin(t *testing.T, manager *Connections, status string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for manager.LoginStatus().Status != status {
		if time.Now().After(deadline) {
			t.Fatalf("login state %q, want %q", manager.LoginStatus().Status, status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestChatGPTSignInModelChoiceAndSignOut(t *testing.T) {
	codex, home := fakeCodex(t, fakeScenario{Output: `{"status":"ok"}`}, false)
	prefs := &memoryPreferences{model: "stale-model"}
	manager, err := NewConnections("", "", "", codex, prefs)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Restore(t.Context()); err != nil || manager.Status().Connected {
		t.Fatalf("restored without a sign-in: %v", err)
	}
	state, err := manager.StartLogin()
	if err != nil || state.Status != "pending" || state.UserCode != "ABCD-1234" ||
		state.URL != "https://auth.openai.com/codex/device" {
		t.Fatalf("unexpected device login %+v, %v", state, err)
	}
	waitForLogin(t, manager, "connected")
	status := manager.Status()
	if !status.Connected || status.Mode != "chatgpt" || status.Account != "teacher@example.com" ||
		status.Model != "" || !manager.Ready() || prefs.model != "" {
		t.Fatalf("sign-in did not connect with the default model: %+v", status)
	}
	if err := manager.Test(t.Context(), status.Version); err != nil || !manager.Status().Verified {
		t.Fatalf("connection test failed: %v", err)
	}
	models, err := manager.Models(t.Context())
	if err != nil || len(models) != 1 || models[0].ID != "gpt-fixture" {
		t.Fatalf("hidden models listed or list failed: %+v, %v", models, err)
	}
	if err := manager.SelectModel(t.Context(), "unknown", manager.Status().Version); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("accepted a model the account cannot use: %v", err)
	}
	if err := manager.SelectModel(t.Context(), "gpt-fixture", manager.Status().Version); err != nil ||
		manager.Status().Model != "gpt-fixture" || prefs.model != "gpt-fixture" {
		t.Fatalf("model choice not applied and kept: %v", err)
	}

	restarted, _ := NewConnections("", "", "", codex, prefs)
	if err := restarted.Restore(t.Context()); err != nil || restarted.Status().Model != "gpt-fixture" ||
		restarted.Status().Account != "teacher@example.com" {
		t.Fatalf("sign-in not restored after restart: %+v, %v", restarted.Status(), err)
	}

	signedOut, err := manager.Disconnect(t.Context(), manager.Status().Version)
	if err != nil || !signedOut || manager.Ready() || prefs.model != "" {
		t.Fatalf("disconnect left the connection: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("Codex sign-in was not removed")
	}
}

func TestEnvironmentAPIKeyTakesPrecedenceOverStoredSignIn(t *testing.T) {
	codex, _ := fakeCodex(t, fakeScenario{}, true)
	manager, err := NewConnections("https://provider.example/v1/chat/completions", "fixture", "key", codex, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Restore(t.Context()); err != nil || manager.Status().Mode != "api_key" {
		t.Fatalf("stored sign-in replaced the configured API key: %+v", manager.Status())
	}
}

func TestFailedSignInKeepsCurrentConnection(t *testing.T) {
	codex, _ := fakeCodex(t, fakeScenario{LoginFail: true}, false)
	manager, _ := NewConnections("", "", "", codex, nil)
	if _, err := manager.StartLogin(); err != nil {
		t.Fatal(err)
	}
	waitForLogin(t, manager, "failed")
	if manager.Status().Connected {
		t.Fatal("failed sign-in connected")
	}
}
