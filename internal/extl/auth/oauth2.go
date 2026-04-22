package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

// skew is the window before ExpiresAt during which a token is treated as
// already expired. Matches golang.org/x/oauth2.
const skew = 60 * time.Second

// OAuth2Provider implements AuthProvider against any OAuth 2.0 +
// PKCE-capable server.
type OAuth2Provider struct {
	pluginID string
	cfg      OAuth2Config
	store    *credman.TokenManager
	flows    *FlowRegistry
	client   *http.Client

	refreshLocks sync.Map // types.TokenKey → *sync.Mutex
}

// NewOAuth2Provider builds a provider. cfg must already be normalized.
func NewOAuth2Provider(pluginID string, cfg OAuth2Config, store *credman.TokenManager, flows *FlowRegistry) *OAuth2Provider {
	return &OAuth2Provider{
		pluginID: pluginID,
		cfg:      cfg,
		store:    store,
		flows:    flows,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Config returns the normalized config (read-only).
func (p *OAuth2Provider) Config() OAuth2Config { return p.cfg }

// FlowRegistry returns the registry used by this provider (so the RPC
// layer can Resolve flows).
func (p *OAuth2Provider) FlowRegistry() *FlowRegistry { return p.flows }

// Token returns a valid access token, refreshing if possible, triggering
// a flow if necessary.
func (p *OAuth2Provider) Token(ctx context.Context, key types.TokenKey, scopes []string) (string, error) {
	key = key.WithDefaultAccount()
	if err := p.scopesAllowed(scopes); err != nil {
		return "", err
	}

	tok, err := p.store.Get(key)
	switch {
	case err != nil:
		// miss → flow
		return p.triggerFlowAndAwait(ctx, key, scopes)
	case !tok.HasScopes(resolveScopes(scopes, p.cfg.Scopes)):
		return p.triggerFlowAndAwait(ctx, key, scopes)
	case !tok.IsExpired(skew):
		return tok.AccessToken, nil
	}

	// Expired. Try refresh.
	newTok, err := p.refreshUnderLock(ctx, key)
	if err == nil {
		return newTok.AccessToken, nil
	}
	// Refresh failed → drop credential and start a flow.
	_ = p.store.Delete(key)
	return p.triggerFlowAndAwait(ctx, key, scopes)
}

// Invalidate blanks the cached access token; refresh token (if any) stays.
func (p *OAuth2Provider) Invalidate(key types.TokenKey) error {
	key = key.WithDefaultAccount()
	tok, err := p.store.Get(key)
	if err != nil {
		return nil // nothing to invalidate
	}
	tok.AccessToken = ""
	tok.ExpiresAt = time.Time{}
	return p.store.Set(key, tok)
}

// Logout calls revoke_url if configured, then deletes from the store.
func (p *OAuth2Provider) Logout(ctx context.Context, key types.TokenKey) error {
	key = key.WithDefaultAccount()
	tok, _ := p.store.Get(key)
	if tok != nil && p.cfg.RevokeURL != "" {
		tokenToRevoke := tok.RefreshToken
		if tokenToRevoke == "" {
			tokenToRevoke = tok.AccessToken
		}
		_ = p.postRevoke(ctx, tokenToRevoke) // best-effort
	}
	return p.store.Delete(key)
}

// ListAccounts returns account labels for this plugin only.
func (p *OAuth2Provider) ListAccounts() []string {
	all := p.store.List()
	out := make([]string, 0, len(all))
	for _, k := range all {
		if k.PluginID == p.pluginID {
			out = append(out, k.Account)
		}
	}
	return out
}

// BuildAuthorizeURL constructs the redirect target for a PKCE flow.
func (p *OAuth2Provider) BuildAuthorizeURL(redirectURI, state, challenge string, scopes []string) string {
	u, _ := url.Parse(p.cfg.AuthorizeURL)
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", strings.Join(resolveScopes(scopes, p.cfg.Scopes), " "))
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", p.cfg.PKCEMethod)
	for k, v := range p.cfg.ExtraAuthParams {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// ExchangeCode completes the PKCE authorization-code grant and stores
// the resulting bundle.
func (p *OAuth2Provider) ExchangeCode(ctx context.Context, key types.TokenKey, code, verifier, redirectURI string) (*types.OAuth2Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("code_verifier", verifier)
	tok, err := p.postToken(ctx, form, nil)
	if err != nil {
		return nil, err
	}
	if err := p.store.Set(key, tok); err != nil {
		return nil, err
	}
	return tok, nil
}

// refreshUnderLock serialises refreshes for the same key.
func (p *OAuth2Provider) refreshUnderLock(ctx context.Context, key types.TokenKey) (*types.OAuth2Token, error) {
	mu := p.muFor(key)
	mu.Lock()
	defer mu.Unlock()
	// Re-read: someone may have refreshed while we waited.
	tok, err := p.store.Get(key)
	if err != nil {
		return nil, err
	}
	if !tok.IsExpired(skew) {
		return tok, nil
	}
	if tok.RefreshToken == "" {
		return nil, errors.New("no refresh token")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", tok.RefreshToken)
	form.Set("client_id", p.cfg.ClientID)
	refreshed, err := p.postToken(ctx, form, tok)
	if err != nil {
		return nil, err
	}
	if err := p.store.Set(key, refreshed); err != nil {
		return nil, err
	}
	return refreshed, nil
}

func (p *OAuth2Provider) muFor(key types.TokenKey) *sync.Mutex {
	m, _ := p.refreshLocks.LoadOrStore(key, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func (p *OAuth2Provider) postToken(ctx context.Context, form url.Values, prior *types.OAuth2Token) (*types.OAuth2Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%w: %s: %s", ErrProvider, resp.Status, string(body))
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		IDToken      string `json:"id_token"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrProvider, err)
	}
	if payload.AccessToken == "" {
		return nil, fmt.Errorf("%w: empty access_token in response", ErrProvider)
	}
	scopes := p.cfg.Scopes
	if payload.Scope != "" {
		scopes = strings.Fields(payload.Scope)
	}
	refresh := payload.RefreshToken
	if refresh == "" && prior != nil {
		refresh = prior.RefreshToken
	}
	now := time.Now()
	return &types.OAuth2Token{
		AccessToken:  payload.AccessToken,
		RefreshToken: refresh,
		TokenType:    payload.TokenType,
		ExpiresAt:    now.Add(time.Duration(payload.ExpiresIn) * time.Second),
		Scopes:       scopes,
		IDToken:      payload.IDToken,
		IssuedAt:     now,
	}, nil
}

func (p *OAuth2Provider) postRevoke(ctx context.Context, token string) error {
	form := url.Values{}
	form.Set("token", token)
	form.Set("client_id", p.cfg.ClientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.RevokeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (p *OAuth2Provider) scopesAllowed(want []string) error {
	allowed := make(map[string]struct{}, len(p.cfg.Scopes))
	for _, s := range p.cfg.Scopes {
		allowed[s] = struct{}{}
	}
	for _, w := range want {
		if _, ok := allowed[w]; !ok {
			return fmt.Errorf("%w: scope %q not declared in manifest", ErrScopeDenied, w)
		}
	}
	return nil
}

// triggerFlowAndAwait starts a flow and blocks on it. The RPC layer (or
// an interactive CLI) is responsible for calling Resolve on the flow id.
func (p *OAuth2Provider) triggerFlowAndAwait(ctx context.Context, key types.TokenKey, scopes []string) (string, error) {
	f, _, err := p.flows.Start(key, FlowKindPKCE)
	if err != nil {
		return "", err
	}
	// Fill in the PKCE details the RPC layer will read off of the flow.
	verifier, err := NewPKCEVerifier()
	if err != nil {
		return "", err
	}
	state, err := randomID()
	if err != nil {
		return "", err
	}
	f.CodeVerifier = verifier
	f.State = state

	select {
	case <-ctx.Done():
		p.flows.Cancel(f.ID, ctx.Err())
		return "", ctx.Err()
	default:
	}

	tok, err := p.flows.Await(f.ID)
	if err != nil {
		return "", err
	}
	if err := p.store.Set(key, tok); err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

// resolveScopes returns `request` when non-empty, else the full manifest set.
func resolveScopes(request, manifest []string) []string {
	if len(request) > 0 {
		return request
	}
	return manifest
}
