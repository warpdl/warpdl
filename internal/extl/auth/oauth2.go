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
	key.PluginID = p.pluginID
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
	// Only nuke the credential on a definitive IdP rejection. Transient
	// errors (network, 5xx) leave the stored token alone so a retry after
	// network recovery can use the still-valid refresh token.
	if errors.Is(err, ErrInvalidGrant) {
		_ = p.store.Delete(key)
		return p.triggerFlowAndAwait(ctx, key, scopes)
	}
	return "", err
}

// Invalidate blanks the cached access token; refresh token (if any) stays.
func (p *OAuth2Provider) Invalidate(key types.TokenKey) error {
	key.PluginID = p.pluginID
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
	key.PluginID = p.pluginID
	key = key.WithDefaultAccount()
	tok, _ := p.store.Get(key)
	if tok != nil && p.cfg.RevokeURL != "" {
		tokenToRevoke := tok.RefreshToken
		if tokenToRevoke == "" {
			tokenToRevoke = tok.AccessToken
		}
		_ = p.postRevoke(ctx, tokenToRevoke) // best-effort
	}
	err := p.store.Delete(key)
	// Drop the refresh mutex too; otherwise the sync.Map entry lingers
	// for the full process lifetime every time an account logs out.
	p.refreshLocks.Delete(key)
	return err
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
		var errPayload struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &errPayload)
		base := ErrProvider
		switch errPayload.Error {
		case "invalid_grant", "invalid_client", "unauthorized_client",
			"invalid_request", "unsupported_grant_type":
			base = ErrInvalidGrant
		}
		msg := errPayload.Error
		if errPayload.Description != "" {
			msg = errPayload.Error + ": " + errPayload.Description
		}
		if msg == "" {
			msg = string(body)
		}
		return nil, fmt.Errorf("%w: %s: %s", base, resp.Status, msg)
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
	switch {
	case payload.Scope != "":
		scopes = strings.Fields(payload.Scope)
	case prior != nil:
		// Server omitted scope on refresh (common). Preserve the
		// actually-granted set from the prior token rather than widening
		// to the manifest — user may have consented to a narrower set.
		scopes = prior.Scopes
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

	// Pre-check is still useful to avoid starting any work after cancellation,
	// but the real guard is the select around the Await.
	select {
	case <-ctx.Done():
		p.flows.Cancel(f.ID, ctx.Err())
		return "", ctx.Err()
	default:
	}

	type awaitResult struct {
		tok *types.OAuth2Token
		err error
	}
	resCh := make(chan awaitResult, 1)
	go func() {
		t, e := p.flows.Await(f.ID)
		resCh <- awaitResult{tok: t, err: e}
	}()

	select {
	case <-ctx.Done():
		p.flows.Cancel(f.ID, ctx.Err())
		// Drain the goroutine — Cancel causes Await to return.
		<-resCh
		return "", ctx.Err()
	case r := <-resCh:
		if r.err != nil {
			return "", r.err
		}
		if err := p.store.Set(key, r.tok); err != nil {
			return "", err
		}
		return r.tok.AccessToken, nil
	}
}

// DeviceAuthorization is the /device/code response.
type DeviceAuthorization struct {
	DeviceCode      string
	UserCode        string
	VerificationURL string
	ExpiresIn       int
	Interval        int
}

// StartDeviceCode initiates the device flow and returns details to show the user.
func (p *OAuth2Provider) StartDeviceCode(ctx context.Context) (*DeviceAuthorization, error) {
	if p.cfg.DeviceURL == "" {
		return nil, fmt.Errorf("device flow not supported by this plugin")
	}
	form := url.Values{}
	form.Set("client_id", p.cfg.ClientID)
	form.Set("scope", strings.Join(p.cfg.Scopes, " "))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.DeviceURL, strings.NewReader(form.Encode()))
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
		return nil, fmt.Errorf("%w: device/code: %s: %s", ErrProvider, resp.Status, string(body))
	}
	var raw struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURL string `json:"verification_url"`
		VerificationURI string `json:"verification_uri"` // Google / RFC variant
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("%w: decode device/code: %v", ErrProvider, err)
	}
	verification := raw.VerificationURL
	if verification == "" {
		verification = raw.VerificationURI
	}
	if raw.Interval == 0 {
		raw.Interval = 5
	}
	return &DeviceAuthorization{
		DeviceCode:      raw.DeviceCode,
		UserCode:        raw.UserCode,
		VerificationURL: verification,
		ExpiresIn:       raw.ExpiresIn,
		Interval:        raw.Interval,
	}, nil
}

// PollDeviceCode polls /token until the user completes the flow or the
// context is cancelled. Handles authorization_pending and slow_down
// responses per RFC 8628.
func (p *OAuth2Provider) PollDeviceCode(ctx context.Context, auth *DeviceAuthorization) (*types.OAuth2Token, error) {
	interval := time.Duration(auth.Interval) * time.Second
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		form := url.Values{}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		form.Set("device_code", auth.DeviceCode)
		form.Set("client_id", p.cfg.ClientID)
		tok, err := p.postToken(ctx, form, nil)
		if err == nil {
			return tok, nil
		}
		errStr := strings.ToLower(err.Error())
		switch {
		case strings.Contains(errStr, "authorization_pending"):
			continue
		case strings.Contains(errStr, "slow_down"):
			interval += 5 * time.Second
			continue
		default:
			return nil, err
		}
	}
}

// resolveScopes returns `request` when non-empty, else the full manifest set.
func resolveScopes(request, manifest []string) []string {
	if len(request) > 0 {
		return request
	}
	return manifest
}
