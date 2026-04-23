package warplib

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// TestBuildPluginHeaderSet covers the canonical-name extraction logic.
func TestBuildPluginHeaderSet(t *testing.T) {
	t.Run("nil headers returns nil", func(t *testing.T) {
		if got := buildPluginHeaderSet(nil); got != nil {
			t.Errorf("buildPluginHeaderSet(nil) = %v, want nil", got)
		}
	})
	t.Run("empty headers returns nil", func(t *testing.T) {
		if got := buildPluginHeaderSet(Headers{}); got != nil {
			t.Errorf("buildPluginHeaderSet(empty) = %v, want nil", got)
		}
	})
	t.Run("only empty-key entries returns nil", func(t *testing.T) {
		hdrs := Headers{{Key: "", Value: "v1"}, {Key: "", Value: "v2"}}
		if got := buildPluginHeaderSet(hdrs); got != nil {
			t.Errorf("buildPluginHeaderSet(empty keys) = %v, want nil", got)
		}
	})
	t.Run("canonicalizes header keys", func(t *testing.T) {
		hdrs := Headers{
			{Key: "authorization", Value: "Bearer x"},
			{Key: "X-CUSTOM-TOKEN", Value: "abc"},
			{Key: "x-api-key", Value: "k"},
		}
		got := buildPluginHeaderSet(hdrs)
		if len(got) != 3 {
			t.Fatalf("expected 3 entries, got %d: %v", len(got), got)
		}
		for _, want := range []string{"Authorization", "X-Custom-Token", "X-Api-Key"} {
			if _, ok := got[want]; !ok {
				t.Errorf("expected canonical key %q in set, got keys: %v", want, got)
			}
		}
	})
	t.Run("deduplicates keys differing only in case", func(t *testing.T) {
		hdrs := Headers{
			{Key: "Authorization", Value: "Bearer x"},
			{Key: "AUTHORIZATION", Value: "Bearer y"},
		}
		got := buildPluginHeaderSet(hdrs)
		if len(got) != 1 {
			t.Errorf("expected 1 entry after dedup, got %d: %v", len(got), got)
		}
	})
	t.Run("skips empty keys among valid ones", func(t *testing.T) {
		hdrs := Headers{
			{Key: "", Value: "skip"},
			{Key: "X-Token", Value: "k"},
		}
		got := buildPluginHeaderSet(hdrs)
		if len(got) != 1 {
			t.Errorf("expected 1 valid entry, got %d", len(got))
		}
	})
}

// TestMergePluginHeaders verifies plugin headers are merged into a
// user-supplied Headers slice with plugin values winning.
func TestMergePluginHeaders(t *testing.T) {
	t.Run("nil plugin is a no-op", func(t *testing.T) {
		user := Headers{{Key: "User-Agent", Value: "A"}}
		mergePluginHeaders(&user, nil)
		if len(user) != 1 || user[0].Value != "A" {
			t.Errorf("expected unchanged user headers, got %v", user)
		}
	})
	t.Run("empty plugin is a no-op", func(t *testing.T) {
		user := Headers{{Key: "User-Agent", Value: "A"}}
		mergePluginHeaders(&user, Headers{})
		if len(user) != 1 {
			t.Errorf("expected unchanged user headers, got %v", user)
		}
	})
	t.Run("plugin override wins on conflict", func(t *testing.T) {
		user := Headers{{Key: "Authorization", Value: "user-token"}}
		plugin := Headers{{Key: "Authorization", Value: "plugin-token"}}
		mergePluginHeaders(&user, plugin)
		if len(user) != 1 {
			t.Fatalf("expected 1 header, got %d: %v", len(user), user)
		}
		if user[0].Value != "plugin-token" {
			t.Errorf("expected plugin-token (plugin wins), got %q", user[0].Value)
		}
	})
	t.Run("plugin appends new keys", func(t *testing.T) {
		user := Headers{{Key: "User-Agent", Value: "UA"}}
		plugin := Headers{{Key: "Authorization", Value: "Bearer x"}}
		mergePluginHeaders(&user, plugin)
		if len(user) != 2 {
			t.Fatalf("expected 2 headers, got %d: %v", len(user), user)
		}
	})
	t.Run("skips empty plugin keys", func(t *testing.T) {
		user := Headers{{Key: "User-Agent", Value: "UA"}}
		plugin := Headers{{Key: "", Value: "junk"}, {Key: "X-Token", Value: "t"}}
		mergePluginHeaders(&user, plugin)
		if len(user) != 2 {
			t.Fatalf("expected 2 headers, got %d: %v", len(user), user)
		}
		for _, h := range user {
			if h.Key == "" {
				t.Error("empty-key plugin header should have been skipped")
			}
		}
	})
}

// TestStripUnsafeFromHeadersCrossOrigin covers the core redirect-strip
// function with explicit plugin-header sets.
func TestStripUnsafeFromHeadersCrossOrigin(t *testing.T) {
	t.Run("strips Authorization (always unsafe)", func(t *testing.T) {
		hdrs := Headers{
			{Key: "Authorization", Value: "Bearer x"},
			{Key: "User-Agent", Value: "UA"},
		}
		out := StripUnsafeFromHeadersCrossOrigin(hdrs, nil)
		if containsKey(out, "Authorization") {
			t.Error("Authorization should be stripped")
		}
		if !containsKey(out, "User-Agent") {
			t.Error("User-Agent should be preserved")
		}
	})
	t.Run("strips plugin-supplied safe-list headers", func(t *testing.T) {
		// User-Agent is normally "safe" (preserved on cross-origin), but
		// if it came from a plugin, it must still be stripped.
		hdrs := Headers{
			{Key: "User-Agent", Value: "PluginUA/1.0"},
			{Key: "Accept", Value: "application/json"},
		}
		names := map[string]struct{}{"User-Agent": {}}
		out := StripUnsafeFromHeadersCrossOrigin(hdrs, names)
		if containsKey(out, "User-Agent") {
			t.Error("plugin-supplied User-Agent should be stripped")
		}
		if !containsKey(out, "Accept") {
			t.Error("non-plugin Accept header should be preserved")
		}
	})
	t.Run("preserves safe headers not in plugin set", func(t *testing.T) {
		hdrs := Headers{
			{Key: "User-Agent", Value: "UA"},
			{Key: "Accept", Value: "*/*"},
			{Key: "Accept-Language", Value: "en"},
			{Key: "Accept-Encoding", Value: "gzip"},
			{Key: "Range", Value: "bytes=0-1023"},
		}
		out := StripUnsafeFromHeadersCrossOrigin(hdrs, nil)
		for _, want := range []string{"User-Agent", "Accept", "Accept-Language", "Accept-Encoding", "Range"} {
			if !containsKey(out, want) {
				t.Errorf("safe header %s should be preserved", want)
			}
		}
	})
	t.Run("case-insensitive plugin name match", func(t *testing.T) {
		hdrs := Headers{
			{Key: "user-agent", Value: "PluginUA"},
		}
		names := map[string]struct{}{"User-Agent": {}}
		out := StripUnsafeFromHeadersCrossOrigin(hdrs, names)
		if len(out) != 0 {
			t.Errorf("case-insensitive plugin name should strip, got %v", out)
		}
	})
	t.Run("does not mutate input slice", func(t *testing.T) {
		hdrs := Headers{
			{Key: "Authorization", Value: "Bearer x"},
			{Key: "User-Agent", Value: "UA"},
		}
		original := make(Headers, len(hdrs))
		copy(original, hdrs)
		_ = StripUnsafeFromHeadersCrossOrigin(hdrs, nil)
		if len(hdrs) != len(original) {
			t.Errorf("input length changed: %d -> %d", len(original), len(hdrs))
		}
		for i, h := range hdrs {
			if h != original[i] {
				t.Errorf("input mutated at index %d", i)
			}
		}
	})
	t.Run("empty input returns empty output", func(t *testing.T) {
		out := StripUnsafeFromHeadersCrossOrigin(Headers{}, nil)
		if len(out) != 0 {
			t.Errorf("empty input should return empty output, got %v", out)
		}
	})
}

// TestStripUnsafeFromHeadersBackwardCompat asserts the legacy entrypoint
// still behaves identically (regression guard for callers that didn't pass
// a plugin-name set).
func TestStripUnsafeFromHeadersBackwardCompat(t *testing.T) {
	hdrs := Headers{
		{Key: "Authorization", Value: "Bearer x"},
		{Key: "User-Agent", Value: "UA"},
		{Key: "X-Custom", Value: "x"},
	}
	out := StripUnsafeFromHeaders(hdrs)
	if containsKey(out, "Authorization") {
		t.Error("Authorization should be stripped")
	}
	if containsKey(out, "X-Custom") {
		t.Error("X-Custom should be stripped")
	}
	if !containsKey(out, "User-Agent") {
		t.Error("User-Agent should be preserved")
	}
}

// TestStripPluginHeaders_Request verifies the request-level helper
// mutates only the targeted keys.
func TestStripPluginHeaders_Request(t *testing.T) {
	req := &http.Request{
		URL:    &url.URL{Scheme: "https", Host: "dest.example.com"},
		Header: make(http.Header),
	}
	req.Header.Set("X-Plugin-Token", "secret")
	req.Header.Set("USER-AGENT", "PluginUA")
	req.Header.Set("Accept", "text/plain") // user-supplied, should stay
	names := map[string]struct{}{
		"X-Plugin-Token": {},
		"User-Agent":     {},
	}
	stripPluginHeaders(req, names)
	if got := req.Header.Get("X-Plugin-Token"); got != "" {
		t.Errorf("X-Plugin-Token should be stripped, got %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != "" {
		t.Errorf("User-Agent should be stripped, got %q", got)
	}
	if got := req.Header.Get("Accept"); got != "text/plain" {
		t.Errorf("Accept should be preserved, got %q", got)
	}
}

func TestStripPluginHeaders_NoOpOnEmpty(t *testing.T) {
	req := &http.Request{Header: make(http.Header)}
	req.Header.Set("Authorization", "Bearer x")
	stripPluginHeaders(req, nil)
	if got := req.Header.Get("Authorization"); got != "Bearer x" {
		t.Errorf("empty names set should be a no-op, got Authorization=%q", got)
	}
	stripPluginHeaders(req, map[string]struct{}{})
	if got := req.Header.Get("Authorization"); got != "Bearer x" {
		t.Errorf("empty names set should be a no-op, got Authorization=%q", got)
	}
}

// TestPluginHeaderCtx verifies the context attach/read round-trip.
func TestPluginHeaderCtx(t *testing.T) {
	t.Run("read from ctx returns attached set", func(t *testing.T) {
		names := map[string]struct{}{"Authorization": {}}
		ctx := WithPluginHeaderNames(context.Background(), names)
		got := pluginHeaderNamesFromCtx(ctx)
		if len(got) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(got))
		}
		if _, ok := got["Authorization"]; !ok {
			t.Error("expected Authorization key in ctx-attached set")
		}
	})
	t.Run("empty set attach returns ctx unchanged", func(t *testing.T) {
		base := context.Background()
		got := WithPluginHeaderNames(base, nil)
		// Value should still return nil
		if pluginHeaderNamesFromCtx(got) != nil {
			t.Error("nil plugin names should produce a ctx with no value")
		}
	})
	t.Run("read from nil ctx returns nil", func(t *testing.T) {
		if got := pluginHeaderNamesFromCtx(nil); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
	t.Run("read from empty ctx returns nil", func(t *testing.T) {
		if got := pluginHeaderNamesFromCtx(context.Background()); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
}

// TestNewDownloader_PluginHeadersSet verifies that PluginHeaders on opts
// are merged into d.headers and their names are recorded for stripping.
func TestNewDownloader_PluginHeadersSet(t *testing.T) {
	// Use a non-redirecting server so NewDownloader can succeed without
	// exercising the cross-origin logic.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		w.Header().Set("Content-Disposition", `attachment; filename="t.bin"`)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	client := &http.Client{}
	d, err := NewDownloader(client, srv.URL+"/file.bin", &DownloaderOpts{
		SkipSetup: true,
		Headers:   Headers{{Key: "Accept", Value: "*/*"}},
		PluginHeaders: Headers{
			{Key: "Authorization", Value: "Bearer plugin-token"},
			{Key: "X-Plugin-Id", Value: "oauth-gdrive"},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader failed: %v", err)
	}

	// Plugin header names must be tracked in canonical form.
	if len(d.pluginHeaderNames) != 2 {
		t.Fatalf("expected 2 plugin header names, got %d: %v", len(d.pluginHeaderNames), d.pluginHeaderNames)
	}
	for _, want := range []string{"Authorization", "X-Plugin-Id"} {
		if _, ok := d.pluginHeaderNames[want]; !ok {
			t.Errorf("expected %s in pluginHeaderNames, got %v", want, d.pluginHeaderNames)
		}
	}

	// Plugin headers must have been merged into d.headers.
	authFound := false
	for _, h := range d.headers {
		if http.CanonicalHeaderKey(h.Key) == "Authorization" {
			authFound = true
			if h.Value != "Bearer plugin-token" {
				t.Errorf("Authorization value = %q, want Bearer plugin-token", h.Value)
			}
		}
	}
	if !authFound {
		t.Error("plugin Authorization header should have been merged into d.headers")
	}
}

// TestNewDownloader_NoPluginHeaders_NilSet asserts that when no plugin
// headers are provided, pluginHeaderNames stays nil (cheap strip path).
func TestNewDownloader_NoPluginHeaders_NilSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		w.Header().Set("Content-Disposition", `attachment; filename="t.bin"`)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	client := &http.Client{}
	d, err := NewDownloader(client, srv.URL+"/file.bin", &DownloaderOpts{
		SkipSetup: true,
	})
	if err != nil {
		t.Fatalf("NewDownloader failed: %v", err)
	}
	if d.pluginHeaderNames != nil {
		t.Errorf("pluginHeaderNames should be nil, got %v", d.pluginHeaderNames)
	}
}

// TestPluginHeaderLeakOnCrossOriginRedirect is the key integration test:
// a plugin-supplied Authorization header must NOT reach a cross-origin
// redirect target.
func TestPluginHeaderLeakOnCrossOriginRedirect(t *testing.T) {
	var mu sync.Mutex
	var finalAuth, finalPluginID string

	// Final server on a different origin — 127.0.0.1 hostname forms a
	// different Host than the initial 127.0.0.1:<port>.
	// httptest uses 127.0.0.1 — they're same host (different port counts
	// as cross-origin, see TestIsCrossOrigin).
	finalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		finalAuth = r.Header.Get("Authorization")
		finalPluginID = r.Header.Get("X-Plugin-Id")
		mu.Unlock()
		w.Header().Set("Content-Length", "5")
		w.Header().Set("Content-Disposition", `attachment; filename="t.bin"`)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Write([]byte("hello"))
	}))
	defer finalSrv.Close()

	initialSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, finalSrv.URL+"/real", http.StatusFound)
	}))
	defer initialSrv.Close()

	// Confirm the two hosts differ (different port → cross-origin under
	// our host-match rule).
	initURL, _ := url.Parse(initialSrv.URL)
	finalURL, _ := url.Parse(finalSrv.URL)
	if !isCrossOrigin(initURL, finalURL) {
		t.Skipf("httptest servers on same host/port %s vs %s; cross-origin not triggered",
			initURL.Host, finalURL.Host)
	}

	client := &http.Client{}
	_, err := NewDownloader(client, initialSrv.URL+"/start", &DownloaderOpts{
		SkipSetup: true,
		PluginHeaders: Headers{
			{Key: "Authorization", Value: "Bearer plugin-token"},
			{Key: "X-Plugin-Id", Value: "oauth-gdrive"},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if finalAuth != "" {
		t.Errorf("plugin-supplied Authorization leaked across origin: got %q", finalAuth)
	}
	if finalPluginID != "" {
		t.Errorf("plugin-supplied X-Plugin-Id leaked across origin: got %q", finalPluginID)
	}
}

// TestPluginHeaderPreservedOnSameOriginRedirect ensures plugin headers
// survive an internal same-origin redirect (Google Drive API style).
func TestPluginHeaderPreservedOnSameOriginRedirect(t *testing.T) {
	var mu sync.Mutex
	var capturedAuth string
	var capturedXPlugin string

	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/file.bin", http.StatusFound)
	})
	mux.HandleFunc("/file.bin", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedAuth = r.Header.Get("Authorization")
		capturedXPlugin = r.Header.Get("X-Plugin-Id")
		mu.Unlock()
		w.Header().Set("Content-Length", "5")
		w.Header().Set("Content-Disposition", `attachment; filename="t.bin"`)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Write([]byte("hello"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &http.Client{}
	_, err := NewDownloader(client, srv.URL+"/start", &DownloaderOpts{
		SkipSetup: true,
		PluginHeaders: Headers{
			{Key: "Authorization", Value: "Bearer plugin-token"},
			{Key: "X-Plugin-Id", Value: "oauth-gdrive"},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if capturedAuth != "Bearer plugin-token" {
		t.Errorf("plugin Authorization should be preserved on same-origin redirect, got %q", capturedAuth)
	}
	if capturedXPlugin != "oauth-gdrive" {
		t.Errorf("plugin X-Plugin-Id should be preserved on same-origin redirect, got %q", capturedXPlugin)
	}
}

// TestPluginVsUserHeaderDivergence exercises the key Task 19 semantic:
// on cross-origin, user-supplied safe headers survive but plugin-supplied
// safe-list headers are stripped.
func TestPluginVsUserHeaderDivergence(t *testing.T) {
	var mu sync.Mutex
	var finalUA string
	var finalReferer string

	finalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		finalUA = r.Header.Get("User-Agent")
		finalReferer = r.Header.Get("Referer")
		mu.Unlock()
		w.Header().Set("Content-Length", "5")
		w.Header().Set("Content-Disposition", `attachment; filename="t.bin"`)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Write([]byte("hello"))
	}))
	defer finalSrv.Close()

	initialSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, finalSrv.URL+"/out", http.StatusFound)
	}))
	defer initialSrv.Close()

	iURL, _ := url.Parse(initialSrv.URL)
	fURL, _ := url.Parse(finalSrv.URL)
	if !isCrossOrigin(iURL, fURL) {
		t.Skip("httptest servers not cross-origin on this platform")
	}

	client := &http.Client{}
	_, err := NewDownloader(client, initialSrv.URL+"/start", &DownloaderOpts{
		SkipSetup: true,
		// User-supplied User-Agent should survive cross-origin.
		Headers: Headers{
			{Key: "User-Agent", Value: "UserUA/1.0"},
		},
		// Plugin-supplied Referer: NOT in safeHeaders anyway (Referer
		// isn't safe), so the standard strip path catches it. We still
		// verify it's stripped.
		PluginHeaders: Headers{
			{Key: "Referer", Value: "https://plugin.example/page"},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// User-Agent: the plugin didn't set it, so it's purely user-supplied.
	// It must survive cross-origin. Note: NewDownloader may override the
	// user's UA with DEF_USER_AGENT if not already set; we set it
	// explicitly above so the user's value must persist.
	if finalUA != "UserUA/1.0" {
		t.Errorf("user-supplied User-Agent should survive cross-origin redirect, got %q", finalUA)
	}
	if finalReferer != "" {
		t.Errorf("plugin-supplied Referer should be stripped, got %q", finalReferer)
	}
}

// TestPluginHeaderOverridesUser verifies that when both user and plugin
// provide the same header, the plugin value wins (plugin is the source
// of truth for auth tokens).
func TestPluginHeaderOverridesUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		w.Header().Set("Content-Disposition", `attachment; filename="t.bin"`)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	client := &http.Client{}
	d, err := NewDownloader(client, srv.URL+"/file", &DownloaderOpts{
		SkipSetup: true,
		Headers: Headers{
			{Key: "Authorization", Value: "Bearer user-token"},
		},
		PluginHeaders: Headers{
			{Key: "Authorization", Value: "Bearer plugin-token"},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader failed: %v", err)
	}

	// d.headers must hold the plugin value.
	var got string
	for _, h := range d.headers {
		if http.CanonicalHeaderKey(h.Key) == "Authorization" {
			got = h.Value
			break
		}
	}
	if got != "Bearer plugin-token" {
		t.Errorf("Authorization should be plugin value, got %q", got)
	}
	// Key should also be registered for cross-origin strip.
	if _, ok := d.pluginHeaderNames["Authorization"]; !ok {
		t.Error("Authorization should be recorded as plugin-supplied")
	}
}

// TestRedirectPolicy_PluginHeadersStrippedViaCtx verifies the CheckRedirect
// callback honors plugin header names via request context.
func TestRedirectPolicy_PluginHeadersStrippedViaCtx(t *testing.T) {
	policy := RedirectPolicy(10)
	ctx := WithPluginHeaderNames(context.Background(), map[string]struct{}{
		"X-Plugin-Token": {},
		"User-Agent":     {},
	})
	via := []*http.Request{
		{URL: &url.URL{Scheme: "https", Host: "a.example.com", Path: "/x"}},
	}
	req := (&http.Request{
		URL:    &url.URL{Scheme: "https", Host: "b.example.com", Path: "/y"},
		Header: make(http.Header),
	}).WithContext(ctx)
	req.Header.Set("X-Plugin-Token", "secret")
	req.Header.Set("User-Agent", "PluginUA")
	req.Header.Set("Accept", "*/*") // not plugin, safe; must survive

	err := policy(req, via)
	if err != nil {
		t.Fatalf("policy returned error: %v", err)
	}
	if got := req.Header.Get("X-Plugin-Token"); got != "" {
		t.Errorf("X-Plugin-Token should be stripped, got %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != "" {
		t.Errorf("plugin-supplied User-Agent should be stripped, got %q", got)
	}
	if got := req.Header.Get("Accept"); got != "*/*" {
		t.Errorf("Accept (not plugin, safe) should survive, got %q", got)
	}
}

// TestRedirectPolicy_SameOriginKeepsPluginHeaders asserts that when the
// redirect is same-origin, plugin headers are NOT stripped (even though
// the ctx carries them).
func TestRedirectPolicy_SameOriginKeepsPluginHeaders(t *testing.T) {
	policy := RedirectPolicy(10)
	ctx := WithPluginHeaderNames(context.Background(), map[string]struct{}{
		"Authorization": {},
	})
	via := []*http.Request{
		{URL: &url.URL{Scheme: "https", Host: "same.example.com", Path: "/a"}},
	}
	req := (&http.Request{
		URL:    &url.URL{Scheme: "https", Host: "same.example.com", Path: "/b"},
		Header: make(http.Header),
	}).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer keep-me")

	err := policy(req, via)
	if err != nil {
		t.Fatalf("policy returned error: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer keep-me" {
		t.Errorf("Authorization should survive same-origin redirect, got %q", got)
	}
}

// TestStripUnsafeFromHeadersCrossOrigin_KnownHeaders sanity-checks the
// legacy callsite: plugin-name-set nil means old behavior.
func TestStripUnsafeFromHeadersCrossOrigin_KnownHeaders(t *testing.T) {
	tests := []struct {
		name     string
		input    Headers
		plugin   map[string]struct{}
		wantKeep []string
		wantDrop []string
	}{
		{
			name: "Cookie always dropped",
			input: Headers{
				{Key: "Cookie", Value: "session=abc"},
				{Key: "User-Agent", Value: "UA"},
			},
			plugin:   nil,
			wantKeep: []string{"User-Agent"},
			wantDrop: []string{"Cookie"},
		},
		{
			name: "canonicalized plugin match drops safe header",
			input: Headers{
				{Key: "range", Value: "bytes=0-"},
				{Key: "accept", Value: "*/*"},
			},
			plugin:   map[string]struct{}{"Range": {}},
			wantKeep: []string{"accept"},
			wantDrop: []string{"range"},
		},
		{
			name: "all-unsafe drops everything",
			input: Headers{
				{Key: "Authorization", Value: "x"},
				{Key: "X-Token", Value: "y"},
			},
			plugin:   nil,
			wantKeep: []string{},
			wantDrop: []string{"Authorization", "X-Token"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := StripUnsafeFromHeadersCrossOrigin(tc.input, tc.plugin)
			for _, k := range tc.wantKeep {
				if !containsKeyCanon(out, k) {
					t.Errorf("expected key %q to be kept, got %v", k, keys(out))
				}
			}
			for _, k := range tc.wantDrop {
				if containsKeyCanon(out, k) {
					t.Errorf("expected key %q to be dropped, got %v", k, keys(out))
				}
			}
		})
	}
}

// --- helpers ---

func containsKey(hdrs Headers, key string) bool {
	canon := http.CanonicalHeaderKey(key)
	for _, h := range hdrs {
		if http.CanonicalHeaderKey(h.Key) == canon {
			return true
		}
	}
	return false
}

func containsKeyCanon(hdrs Headers, key string) bool {
	// Case-insensitive search that tolerates non-canonical storage.
	target := strings.ToLower(key)
	for _, h := range hdrs {
		if strings.ToLower(h.Key) == target {
			return true
		}
	}
	return false
}

func keys(hdrs Headers) []string {
	out := make([]string, len(hdrs))
	for i, h := range hdrs {
		out[i] = h.Key
	}
	return out
}
