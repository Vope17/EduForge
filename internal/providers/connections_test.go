package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"eduforge/internal/domain"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexProtocolHelper(t *testing.T) {
	if os.Getenv("EDUFORGE_PROTOCOL_HELPER") != "1" {
		return
	}
	dec, enc := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Cursor *string `json:"cursor"`
			} `json:"params"`
		}
		if dec.Decode(&req) != nil {
			os.Exit(0)
		}
		if req.ID == 0 {
			continue
		}
		var result any = map[string]any{}
		switch req.Method {
		case "account/read":
			result = map[string]any{"account": map[string]string{"type": "chatgpt"}}
		case "account/login/start":
			result = map[string]string{"userCode": "TEST-CODE", "verificationUrl": "https://auth.openai.com/codex/device", "loginId": "test"}
		case "model/list":
			if req.Params.Cursor != nil && *req.Params.Cursor == "" {
				_ = enc.Encode(map[string]any{"id": req.ID, "error": map[string]any{"code": -32600, "message": "invalid cursor: "}})
				continue
			}
			result = map[string]any{"data": []map[string]any{{"model": "account-model", "displayName": "Account model"}, {"model": "hidden-model", "hidden": true}}}
		}
		_ = enc.Encode(map[string]any{"id": req.ID, "result": result})
		if req.Method == "account/login/start" {
			_ = enc.Encode(map[string]any{"method": "account/login/completed", "params": map[string]bool{"success": true}})
		}
	}
}
func TestOfficialLoginStateAndSubscriptionModelCatalog(t *testing.T) {
	m, owner := managerFixture(t)
	dir := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(binary, "'", "'\"'\"'") + "' -test.run=TestCodexProtocolHelper\n"
	if err = os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("EDUFORGE_PROTOCOL_HELPER", "1")
	ref := providerRecord(t, m, owner, domain.Provider{Kind: "codex"})
	login, err := m.StartLogin(context.Background(), owner, ref.ID)
	if err != nil {
		t.Fatal(err)
	}
	if login.Code != "TEST-CODE" || login.Status != "pending" {
		t.Fatalf("unexpected login %#v", login)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		state, err := m.LoginStatus(context.Background(), owner, ref.ID)
		if err != nil {
			t.Fatal(err)
		}
		if state.Status == "connected" {
			if state.Code != "" {
				t.Fatal("completed login retained device code")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("login did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	models, err := m.Models(context.Background(), owner, ref.ID)
	if err != nil || len(models) != 1 || models[0].ID != "account-model" {
		t.Fatalf("catalog %#v %v", models, err)
	}
	bob, err := m.Store.AddUser(context.Background(), "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.LoginStatus(context.Background(), bob, ref.ID); err == nil {
		t.Fatal("login visible to another owner")
	}
}

type catalogTransport func(*http.Request) (*http.Response, error)

func (f catalogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestAPIModelCatalogUsesSavedCredential(t *testing.T) {
	m, owner := managerFixture(t)
	ref := providerRecord(t, m, owner, domain.Provider{Kind: "openai"})
	if err := m.SetSecret(owner, ref.ID, "test-key"); err != nil {
		t.Fatal(err)
	}
	m.HTTP = &http.Client{Transport: catalogTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("incorrect model request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"available-model"}]}`)), Header: http.Header{}}, nil
	})}
	models, err := m.Models(context.Background(), owner, ref.ID)
	if err != nil || len(models) != 1 || models[0].ID != "available-model" {
		t.Fatalf("catalog %#v %v", models, err)
	}
}
