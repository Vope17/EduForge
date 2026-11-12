package llm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	openAIIssuer      = "https://auth.openai.com"
	responsesResource = "https://api.openai.com/v1"
	responsesEndpoint = responsesResource + "/responses"
	oauthCallbackPath = "/api/v1/model-connection/chatgpt/callback"
)

var (
	ErrOAuthState        = errors.New("invalid or expired authorization attempt")
	ErrAuthorization     = errors.New("ChatGPT authorization failed")
	ErrAuthorizationDeny = errors.New("ChatGPT authorization declined")
	ErrPlanPermission    = errors.New("ChatGPT plan permission not granted")
	ErrSubscriptionLimit = errors.New("ChatGPT plan usage limit reached")
)

type RegistrationStore interface {
	ModelRegistration(context.Context, string) (host, client, subject string, err error)
	SaveModelRegistration(context.Context, string, string, string) error
}

type discovery struct {
	Issuer        string `json:"issuer"`
	Authorization string `json:"authorization_endpoint"`
	Token         string `json:"token_endpoint"`
	JWKS          string `json:"jwks_uri"`
	Revocation    string `json:"revocation_endpoint"`
}

type oauthAttempt struct {
	verifier, nonce, binding, clientID, subject string
	expires                                     time.Time
	version                                     uint64
}

type OAuth struct {
	mu        sync.Mutex
	pending   map[string]oauthAttempt
	discovery *discovery
	keys      oidc.KeySet
	store     RegistrationStore
	manager   *Connections
	client    *http.Client
	origin    string
}

func NewOAuth(registrations RegistrationStore, manager *Connections, origin string) (*OAuth, error) {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return nil, errors.New("EDUFORGE_PUBLIC_ORIGIN must be an HTTP 127.0.0.1 origin for local ChatGPT sign-in")
	}
	return &OAuth{
		pending: make(map[string]oauthAttempt), store: registrations, manager: manager, origin: origin,
		client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}, nil
}

func (o *OAuth) Origin() string { return o.origin }

func randomSecret() string {
	data := make([]byte, 32)
	rand.Read(data)
	return base64.RawURLEncoding.EncodeToString(data)
}

func (o *OAuth) loadDiscovery(ctx context.Context) (discovery, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.discovery != nil {
		return *o.discovery, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, openAIIssuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return discovery{}, ErrAuthorization
	}
	var result discovery
	if err := o.requestJSON(request, &result); err != nil {
		return result, err
	}
	if result.Issuer != openAIIssuer {
		return result, ErrAuthorization
	}
	for _, endpoint := range []string{result.Authorization, result.Token, result.JWKS} {
		if !openAIAuthURL(endpoint) {
			return result, ErrAuthorization
		}
	}
	if result.Revocation != "" && !openAIAuthURL(result.Revocation) {
		return result, ErrAuthorization
	}
	o.keys = oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), o.client), result.JWKS)
	o.discovery = &result
	return result, nil
}

func openAIAuthURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host == "auth.openai.com" &&
		parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

// Start keeps PKCE and nonce on the server. Only the authorization URL goes to the browser.
func (o *OAuth) Start(ctx context.Context, user string, newAccount, requestConsent bool) (authorizationURL, binding string, err error) {
	config, err := o.loadDiscovery(ctx)
	if err != nil {
		return "", "", err
	}
	host, clientID, subject, err := o.store.ModelRegistration(ctx, user)
	if err != nil {
		return "", "", fmt.Errorf("load ChatGPT registration: %w", err)
	}
	if newAccount {
		clientID, subject = "", ""
	}
	if clientID == "" {
		clientID = "dynamic_agent_client"
	}
	state, nonce, verifier, binding := randomSecret(), randomSecret(), randomSecret(), randomSecret()
	challenge := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"client_id": {clientID}, "ext_agent_host_id": {host},
		"response_type": {"code"}, "redirect_uri": {o.origin + oauthCallbackPath},
		"scope":    {"openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"},
		"resource": {responsesResource}, "state": {state}, "nonce": {nonce},
		"code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])},
	}
	if requestConsent {
		query.Set("prompt", "consent")
	}
	if clientID == "dynamic_agent_client" {
		query.Set("agent_name_hint", "EduForge")
	}
	version := o.manager.Status().Version
	o.mu.Lock()
	defer o.mu.Unlock()
	for key, attempt := range o.pending {
		if time.Now().After(attempt.expires) {
			delete(o.pending, key)
		}
	}
	if len(o.pending) >= 8 {
		return "", "", ErrOAuthState
	}
	o.pending[state] = oauthAttempt{
		verifier: verifier, nonce: nonce, binding: binding, clientID: clientID, subject: subject,
		expires: time.Now().Add(10 * time.Minute), version: version,
	}
	return config.Authorization + "?" + query.Encode(), binding, nil
}

func (o *OAuth) Cancel() {
	o.mu.Lock()
	defer o.mu.Unlock()
	clear(o.pending)
}

type tokenSet struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

func (o *OAuth) Complete(ctx context.Context, user, binding string, query url.Values) error {
	if len(query["state"]) != 1 {
		return ErrOAuthState
	}
	state := query.Get("state")
	o.mu.Lock()
	attempt, exists := o.pending[state]
	// A callback from another browser cannot consume this browser's pending attempt.
	if exists && subtle.ConstantTimeCompare([]byte(binding), []byte(attempt.binding)) == 1 {
		delete(o.pending, state)
	} else {
		exists = false
	}
	o.mu.Unlock()
	if !exists || binding == "" || time.Now().After(attempt.expires) {
		return ErrOAuthState
	}
	if query.Get("error") != "" {
		return ErrAuthorizationDeny
	}
	if len(query["code"]) != 1 || query.Get("code") == "" || len(query["client_id"]) > 1 {
		return ErrAuthorization
	}
	clientID := query.Get("client_id")
	if attempt.clientID == "dynamic_agent_client" {
		if !strings.HasPrefix(clientID, "oaiapp_") || len(clientID) > 512 {
			return ErrAuthorization
		}
	} else {
		if clientID != "" && clientID != attempt.clientID {
			return ErrAuthorization
		}
		clientID = attempt.clientID
	}
	config, err := o.loadDiscovery(ctx)
	if err != nil {
		return err
	}
	tokens, err := o.exchange(ctx, config.Token, url.Values{
		"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {query.Get("code")},
		"code_verifier": {attempt.verifier}, "redirect_uri": {o.origin + oauthCallbackPath}, "resource": {responsesResource},
	})
	if err != nil {
		return err
	}
	verifier := oidc.NewVerifier(openAIIssuer, o.keys, &oidc.Config{ClientID: clientID})
	identity, err := verifier.Verify(ctx, tokens.IDToken)
	if err != nil || identity.Subject == "" || identity.Nonce != attempt.nonce ||
		attempt.subject != "" && attempt.subject != identity.Subject {
		return ErrAuthorization
	}
	if err := o.store.SaveModelRegistration(ctx, user, clientID, identity.Subject); err != nil {
		return fmt.Errorf("save ChatGPT registration: %w", err)
	}
	if !slices.Contains(strings.Fields(tokens.Scope), "chatgpt.tokens.use.direct") ||
		!slices.Contains(strings.Fields(tokens.Scope), "resource.invoke") {
		return ErrPlanPermission
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := identity.Claims(&claims); err != nil {
		return ErrAuthorization
	}
	provider := &subscriptionProvider{
		oauth: o, clientID: clientID, tokens: tokens, expires: time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second),
	}
	if err := o.manager.installChatGPT(provider, claims.Email, attempt.version); err != nil {
		revokeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = provider.Revoke(revokeCtx)
		return err
	}
	return nil
}

func (o *OAuth) exchange(ctx context.Context, endpoint string, form url.Values) (tokenSet, error) {
	var tokens tokenSet
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tokens, ErrAuthorization
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := o.requestJSON(request, &tokens); err != nil {
		return tokens, err
	}
	if tokens.AccessToken == "" || !strings.EqualFold(tokens.TokenType, "bearer") || tokens.ExpiresIn <= 0 || tokens.ExpiresIn > 86400 {
		return tokens, ErrAuthorization
	}
	return tokens, nil
}

func (o *OAuth) requestJSON(request *http.Request, target any) (err error) {
	response, err := o.client.Do(request)
	if err != nil {
		return ErrAuthorization
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil && err == nil {
			err = ErrAuthorization
		}
	}()
	if response.StatusCode != http.StatusOK {
		return ErrAuthorization
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 || json.Unmarshal(data, target) != nil {
		return ErrAuthorization
	}
	return nil
}
