package extl

import (
	_ "embed"
	"net/http"
	"strings"

	"github.com/dop251/goja"
)

//go:embed header.js
var headerJs string

func loadHeaderJs(runtime *goja.Runtime) error {
	_, err := runtime.RunString(headerJs)
	return err
}

type Header struct {
	std     http.Header
	runtime *goja.Runtime
}

func (h Header) Append(key, value string) {
	h.std.Add(key, value)
}

func (h Header) Delete(key string) {
	h.std.Del(key)
}

func (h Header) Entries() [][]string {
	v := make([][]string, 0, len(h.std))
	for k, _v := range h.std {
		if strings.EqualFold(k, "Set-Cookie") || len(_v) == 0 {
			continue
		}
		v = append(v, []string{k, _v[0]})
	}
	return v
}

func (h Header) ForEach(callback any) {
	if h.runtime == nil {
		return
	}
	cb, ok := callback.(func(goja.FunctionCall) goja.Value)
	if !ok {
		return
	}
	for k, v := range h.std {
		if strings.EqualFold(k, "Set-Cookie") || len(v) == 0 {
			continue
		}
		cb(goja.FunctionCall{
			Arguments: []goja.Value{
				h.runtime.ToValue(v[0]),
				h.runtime.ToValue(k),
			},
		})
	}
}

func (h Header) Get(key string) string {
	return h.std.Get(key)
}

func (h Header) GetSetCookies() []string {
	return h.std["Set-Cookie"]
}

func (h Header) Size() int {
	return len(h.std)
}

func (h Header) Has(key string) bool {
	return h.std.Get(key) != ""
}

func (h Header) Keys() []string {
	keys := make([]string, 0, len(h.std))
	for k := range h.std {
		if strings.EqualFold(k, "Set-Cookie") {
			continue
		}
		keys = append(keys, k)
	}
	return keys
}

func (h Header) Set(key, value string) {
	h.std.Set(key, value)
}

func (h Header) Values() []string {
	values := make([]string, 0, len(h.std))
	for k, v := range h.std {
		if strings.EqualFold(k, "Set-Cookie") || len(v) == 0 {
			continue
		}
		values = append(values, v[0])
	}
	return values
}
