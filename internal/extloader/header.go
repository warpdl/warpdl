package extloader

import (
	_ "embed"
	"net/http"
	"strings"

	"github.com/dop251/goja"
)

//go:embed header.js
var headerJs string

func loadHeaderJs(runtime *goja.Runtime) {
	runtime.RunString(headerJs)
}

type Header struct {
	std     http.Header
	runtime *goja.Runtime
}

func (h Header) Append(key, value string) {
	v := h.std.Get(key)
	h.std.Set(key, strings.Join([]string{v, value}, ","))
}

func (h Header) Delete(key string) {
	h.std.Del(key)
}

func (h Header) Entries() [][]string {
	v := make([][]string, len(h.std)-1)
	var i int64 = 0
	for k, _v := range h.std {
		if k == "Set-Cookie" {
			continue
		}
		v[i] = []string{k, _v[0]}
		i++
	}
	return v
}

func (h Header) ForEach(callback any) {
	cb, ok := callback.(func(goja.FunctionCall) goja.Value)
	if !ok {
		return
	}
	for k, v := range h.std {
		if k == "Set-Cookie" {
			continue
		}
		cb(goja.FunctionCall{
			Arguments: []goja.Value{
				h.runtime.ToValue(v[0]),
				h.runtime.ToValue(k),
			},
		})
	}
	h.runtime.RunString("throw new Error('Invalid function type')")
}

func (h Header) Get(key string) string {
	return h.std.Get(key)
}

func (h Header) GetSetCookies() []string {
	return h.std["Set-Cookie"]
}

func (h Header) Has(key string) bool {
	return h.std.Get(key) != ""
}

func (h Header) Keys() []string {
	var keys []string = make([]string, len(h.std)-1)
	var i int64 = 0
	for k := range h.std {
		if k == "Set-Cookie" {
			continue
		}
		keys[i] = k
		i++
	}
	return keys
}

func (h Header) Set(key, value string) {
	h.std.Set(key, value)
}

func (h Header) Values() []string {
	var values []string = make([]string, len(h.std)-1)
	var i int64 = 0
	for k, v := range h.std {
		if k == "Set-Cookie" {
			continue
		}
		values[i] = v[0]
		i++
	}
	return values
}
