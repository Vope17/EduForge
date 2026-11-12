package llm

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

type registrationFixture struct {
	client, subject string
}

func (f *registrationFixture) ModelRegistration(context.Context, string) (string, string, string, error) {
	return "urn:uuid:ec57db7e-30f2-4c93-9c7d-58421368b383", f.client, f.subject, nil
}

func (f *registrationFixture) SaveModelRegistration(_ context.Context, _ string, client, subject string) error {
	f.client, f.subject = client, subject
	return nil
}

func fixtureResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func signedIdentity(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"fixture"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	input := header + "." + base64.RawURLEncoding.EncodeToString(body)
	hash := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(signature)
}

type oauthFixture struct {
	oauth       *OAuth
	manager     *Connections
	store       *registrationFixture
	nonce       string
	tokenCalls  int
	revokeError bool
	claims      map[string]any
	scope       string
}

func newOAuthFixture(t *testing.T) *oauthFixture {
	t.Helper()
	manager, err := NewConnections("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	registration := &registrationFixture{}
	flow, err := NewOAuth(registration, manager, "http://127.0.0.1:8081")
	if err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &oauthFixture{
		oauth: flow, manager: manager, store: registration,
		scope: "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct",
		claims: map[string]any{
			"iss": openAIIssuer, "sub": "fixture-subject", "aud": "oaiapp_fixture",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "email": "teacher@example.com",
		},
	}
	flow.discovery = &discovery{
		Issuer: openAIIssuer, Authorization: openAIIssuer + "/api/accounts/authorize",
		Token: openAIIssuer + "/api/accounts/oauth/token", JWKS: openAIIssuer + "/.well-known/jwks.json",
		Revocation: openAIIssuer + "/oauth/revoke",
	}
	flow.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/accounts/oauth/token":
			f.tokenCalls++
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("client_id") != "oaiapp_fixture" || r.Form.Get("resource") != responsesResource {
				t.Fatal("wrong OAuth client or resource")
			}
			if r.Form.Get("grant_type") == "authorization_code" && r.Form.Get("redirect_uri") != flow.origin+oauthCallbackPath {
				t.Fatal("callback URI changed during token exchange")
			}
			claims := make(map[string]any)
			for name, value := range f.claims {
				claims[name] = value
			}
			claims["nonce"] = f.nonce
			encoded, err := json.Marshal(tokenSet{
				AccessToken: "fixture-access", RefreshToken: "fixture-refresh", TokenType: "Bearer",
				ExpiresIn: 3600, Scope: f.scope, IDToken: signedIdentity(t, key, claims),
			})
			if err != nil {
				t.Fatal(err)
			}
			return fixtureResponse(200, string(encoded)), nil
		case "/.well-known/jwks.json":
			n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
			return fixtureResponse(200, `{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":"fixture","n":"`+n+`","e":"AQAB"}]}`), nil
		case "/v1/models":
			if r.Header.Get("Authorization") != "Bearer fixture-access" {
				t.Fatal("missing OAuth bearer token")
			}
			return fixtureResponse(200, `{"models":[{"slug":"fixture-model","display_name":"Fixture model","visibility":"list"},{"slug":"hidden","visibility":"hide"}]}`), nil
		case "/v1/responses":
			var input map[string]any
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input["store"] != false || input["stream"] != true || input["model"] != "fixture-model" {
				t.Fatal("incorrect ChatGPT inference contract")
			}
			return fixtureResponse(200, "data: "+`{"type":"response.completed","response":{"status":"completed","output":[{"content":[{"type":"output_text","text":"{\"status\":\"ok\"}"}]}]}}`+"\n\n"), nil
		case "/oauth/revoke":
			if f.revokeError {
				return fixtureResponse(503, ""), nil
			}
			return fixtureResponse(200, ""), nil
		default:
			t.Fatalf("unexpected external request: %s", r.URL.Path)
			return nil, nil
		}
	})
	flow.keys = oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), flow.client), flow.discovery.JWKS)
	return f
}

func (f *oauthFixture) start(t *testing.T) (string, url.Values) {
	t.Helper()
	target, binding, err := f.oauth.Start(t.Context(), "default", false, false)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("client_id") != "dynamic_agent_client" || query.Get("code_challenge_method") != "S256" ||
		query.Get("ext_agent_host_id") == "" || query.Get("agent_name_hint") != "EduForge" ||
		query.Get("redirect_uri") != f.oauth.origin+oauthCallbackPath {
		t.Fatal("incorrect dynamic-registration request")
	}
	f.nonce = query.Get("nonce")
	state := query.Get("state")
	f.oauth.mu.Lock()
	attempt := f.oauth.pending[state]
	f.oauth.mu.Unlock()
	hash := sha256.Sum256([]byte(attempt.verifier))
	if query.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(hash[:]) {
		t.Fatal("incorrect PKCE challenge")
	}
	return binding, url.Values{"state": {state}, "code": {"fixture-code"}, "client_id": {"oaiapp_fixture"}}
}

func TestChatGPTAuthorizationModelSelectionAndRevocation(t *testing.T) {
	f := newOAuthFixture(t)
	binding, query := f.start(t)
	if err := f.oauth.Complete(t.Context(), "default", "wrong-browser", query); !errors.Is(err, ErrOAuthState) {
		t.Fatal("accepted a callback from another browser")
	}
	if err := f.oauth.Complete(t.Context(), "default", binding, query); err != nil {
		t.Fatal(err)
	}
	if err := f.oauth.Complete(t.Context(), "default", binding, query); !errors.Is(err, ErrOAuthState) {
		t.Fatal("authorization callback was reusable")
	}
	if f.store.client != "oaiapp_fixture" || f.store.subject != "fixture-subject" || f.manager.Ready() {
		t.Fatal("registration was not saved or model was chosen without user input")
	}
	models, err := f.manager.Models(t.Context())
	if err != nil || len(models) != 1 || models[0].ID != "fixture-model" {
		t.Fatalf("model catalog: %+v, %v", models, err)
	}
	if err := f.manager.SelectModel(t.Context(), "hidden", f.manager.Status().Version); !errors.Is(err, ErrConfiguration) {
		t.Fatal("selected a model outside the visible account catalog")
	}
	if err := f.manager.SelectModel(t.Context(), "fixture-model", f.manager.Status().Version); err != nil {
		t.Fatal(err)
	}
	if err := f.manager.Test(t.Context(), f.manager.Status().Version); err != nil || !f.manager.Status().Verified {
		t.Fatalf("test connection: %v", err)
	}
	provider, _, _ := f.manager.subscription()
	provider.mu.Lock()
	provider.expires = time.Now().Add(-time.Minute)
	provider.mu.Unlock()
	if _, err := f.manager.Models(t.Context()); err != nil || f.tokenCalls != 2 {
		t.Fatalf("refresh: %v, calls %d", err, f.tokenCalls)
	}
	f.revokeError = true
	confirmed, err := f.manager.Disconnect(t.Context(), f.manager.Status().Version)
	if err != nil || confirmed || f.manager.Status().Connected || provider.tokens.AccessToken != "" {
		t.Fatal("disconnect retained credentials or claimed remote revocation")
	}
}

func TestInvalidChatGPTIdentityAndMissingPermission(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*oauthFixture)
	}{
		{"wrong audience", func(f *oauthFixture) { f.claims["aud"] = "another-client" }},
		{"wrong issuer", func(f *oauthFixture) { f.claims["iss"] = "https://example.com" }},
		{"expired", func(f *oauthFixture) { f.claims["exp"] = time.Now().Add(-time.Hour).Unix() }},
		{"wrong nonce", func(f *oauthFixture) { f.nonce = "wrong-nonce" }},
		{"missing plan grant", func(f *oauthFixture) { f.scope = "openid profile email" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newOAuthFixture(t)
			binding, query := f.start(t)
			test.change(f)
			if err := f.oauth.Complete(t.Context(), "default", binding, query); err == nil {
				t.Fatal("accepted invalid authorization")
			}
			if f.manager.Status().Connected {
				t.Fatal("invalid authorization replaced the active connection")
			}
		})
	}
}

func TestResponsesRequiresCompletedEvent(t *testing.T) {
	for _, data := range []string{
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n",
		"data: {\"type\":\"response.incomplete\"}\n\n",
		"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"subscription_sharing_usage_limit_exceeded\"}}}\n\n",
	} {
		if _, err := completedResponse(strings.NewReader(data)); err == nil {
			t.Fatal("accepted an incomplete or failed inference stream")
		}
	}
}
