package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/dop251/goja"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

// parseOpts extracts `account` (string) and `scopes` ([]string) from an
// options object passed to a binding. Accepts a JS object with either
// lowercase field names (documented API) or mixed-case. Missing/undefined
// fields return zero values.
func parseOpts(rt *goja.Runtime, v goja.Value) (account string, scopes []string, err error) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return "", nil, nil
	}
	obj, ok := v.(*goja.Object)
	if !ok {
		return "", nil, fmt.Errorf("options must be an object")
	}
	if av := obj.Get("account"); av != nil && !goja.IsUndefined(av) && !goja.IsNull(av) {
		account = av.String()
	}
	if sv := obj.Get("scopes"); sv != nil && !goja.IsUndefined(sv) && !goja.IsNull(sv) {
		var s []string
		if err := rt.ExportTo(sv, &s); err != nil {
			return "", nil, fmt.Errorf("scopes must be a string array: %w", err)
		}
		scopes = s
	}
	return account, scopes, nil
}

// RegisterBindings installs getAccessToken, fetchWithAuth, invalidateToken,
// and listAccounts on runtime, wired to provider. Calls use a background
// context for compatibility with standalone runtimes.
func RegisterBindings(rt *goja.Runtime, p AuthProvider) error {
	return RegisterBindingsWithContext(rt, p, context.Background)
}

// RegisterBindingsWithContext installs the auth bindings and obtains the
// context for each token request from executionContext. Engine-managed
// runtimes use this to propagate their execution deadline into blocking
// authentication flows.
func RegisterBindingsWithContext(
	rt *goja.Runtime,
	p AuthProvider,
	executionContext func() context.Context,
) error {
	if executionContext == nil {
		executionContext = context.Background
	}
	getToken := func(call goja.FunctionCall) goja.Value {
		var arg goja.Value
		if len(call.Arguments) > 0 {
			arg = call.Arguments[0]
		}
		account, scopes, err := parseOpts(rt, arg)
		if err != nil {
			panic(rt.NewTypeError("getAccessToken: " + err.Error()))
		}
		key := types.TokenKey{Account: account}
		ctx := executionContext()
		if ctx == nil {
			ctx = context.Background()
		}
		tok, err := p.Token(ctx, key, scopes)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
				if cause := context.Cause(ctx); cause != nil {
					err = cause
				}
			}
			panic(rt.NewGoError(err))
		}
		return rt.ToValue(tok)
	}
	if err := rt.Set("getAccessToken", getToken); err != nil {
		return err
	}

	invalidate := func(call goja.FunctionCall) goja.Value {
		var arg goja.Value
		if len(call.Arguments) > 0 {
			arg = call.Arguments[0]
		}
		account, _, err := parseOpts(rt, arg)
		if err != nil {
			panic(rt.NewTypeError("invalidateToken: " + err.Error()))
		}
		key := types.TokenKey{Account: account}
		if err := p.Invalidate(key); err != nil {
			panic(rt.NewGoError(err))
		}
		return goja.Undefined()
	}
	if err := rt.Set("invalidateToken", invalidate); err != nil {
		return err
	}

	listFn := func(call goja.FunctionCall) goja.Value {
		return rt.ToValue(p.ListAccounts())
	}
	if err := rt.Set("listAccounts", listFn); err != nil {
		return err
	}

	// fetchWithAuth: thin JS wrapper around the existing `request` global
	// that adds Authorization and handles one 401 retry after refresh.
	// If `request` isn't installed on this runtime, throw at call time.
	fetchJs := `
function fetchWithAuth(req, opts) {
    if (typeof request !== "function") {
        throw new Error("fetchWithAuth: request() not available");
    }
    var scopes = (opts && opts.scopes) || undefined;
    var token = getAccessToken({scopes: scopes, account: opts && opts.account});
    var headers = Object.assign({}, req.headers || {});
    headers["Authorization"] = "Bearer " + token;
    var merged = Object.assign({}, req, {headers: headers});
    var resp = request(merged);
    // Auto-retry once on 401 with a fresh token.
    if (resp && resp.status_code === 401) {
        invalidateToken({account: opts && opts.account});
        var token2 = getAccessToken({scopes: scopes, account: opts && opts.account});
        headers["Authorization"] = "Bearer " + token2;
        resp = request(Object.assign({}, req, {headers: headers}));
    }
    return resp;
}`
	if _, err := rt.RunString(fetchJs); err != nil {
		return fmt.Errorf("install fetchWithAuth: %w", err)
	}
	return nil
}
