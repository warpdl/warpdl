package extloader

import (
	_ "embed"
	"io"
	"net/http"
	"strings"

	"github.com/dop251/goja"
)

//go:embed request.js
var requestJs string

func loadRequestJs(runtime *goja.Runtime) {
	runtime.RunString(requestJs)
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

func _requestCallback(runtime *goja.Runtime, client *http.Client) func(goja.FunctionCall) goja.Value {
	return func(v goja.FunctionCall) goja.Value {
		if len(v.Arguments) != 1 {
			throw(runtime, "invalid number of arguments")
			// runtime.NewGoError(errors.New("invalid number of arguments"))
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
		resp, err := client.Do(req)
		if err != nil {
			throw(runtime, err.Error())
			return nil
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
				std: resp.Header,
			},
		})
	}
}
