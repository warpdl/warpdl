package extl

import (
	"context"
	_ "embed"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dop251/goja"
)

//go:embed request.js
var requestJs string

func loadRequestJs(runtime *goja.Runtime) error {
	_, err := runtime.RunString(requestJs)
	return err
}

type Request struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

type Response struct {
	ContentLength int64   `json:"content_length"`
	Body          string  `json:"body"`
	StatusCode    int     `json:"status_code"`
	Headers       *Header `json:"headers"`
}

func _requestCallback(
	runtime *goja.Runtime,
	client *http.Client,
	executionContext ...func() context.Context,
) func(goja.FunctionCall) goja.Value {
	return requestCallback(runtime, client, defaultRequestTimeout, executionContext...)
}

func requestCallback(
	runtime *goja.Runtime,
	client *http.Client,
	timeout time.Duration,
	executionContext ...func() context.Context,
) func(goja.FunctionCall) goja.Value {
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	currentContext := context.Background
	if len(executionContext) > 0 && executionContext[0] != nil {
		currentContext = executionContext[0]
	}
	return func(v goja.FunctionCall) goja.Value {
		if len(v.Arguments) != 1 {
			throw(runtime, "invalid number of arguments")
			return nil
		}
		var r Request
		err := runtime.ExportTo(v.Arguments[0], &r)
		if err != nil {
			throw(runtime, err.Error())
			return nil
		}
		req, err := http.NewRequest(r.Method, r.URL, strings.NewReader(r.Body))
		if err != nil {
			throw(runtime, err.Error())
			return nil
		}
		for k, v := range r.Headers {
			req.Header.Add(k, v)
		}
		parentCtx := currentContext()
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(parentCtx, timeout)
		defer cancel()
		resp, err := client.Do(req.WithContext(ctx))
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
				if cause := context.Cause(ctx); cause != nil {
					err = cause
				}
			}
			panic(runtime.NewGoError(err))
		}
		defer resp.Body.Close()
		lr := io.LimitReader(resp.Body, 1024*1024)
		b, err := io.ReadAll(lr)
		if err != nil {
			throw(runtime, err.Error())
			return nil
		}
		return runtime.ToValue(Response{
			ContentLength: resp.ContentLength,
			Body:          string(b),
			StatusCode:    resp.StatusCode,
			Headers: &Header{
				std:     resp.Header,
				runtime: runtime,
			},
		})
	}
}
