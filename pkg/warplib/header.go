package warplib

import "net/http"

const (
	// Header keys
	USER_AGENT_KEY = "User-Agent"
)

// Headers represents a list of headers.
type Headers []Header

// Get returns the index of the header with the given key.
// If the header is not found, the second return value is false.
func (h Headers) Get(key string) (index int, have bool) {
	for i, x := range h {
		if x.Key != key {
			continue
		}
		index = i
		have = true
		break
	}
	return
}

// InitOrUpdate initializes or updates the header with the given key and value.
// If the header is already present, it is not updated.
func (h *Headers) InitOrUpdate(key, value string) {
	_, ok := h.Get(key)
	if ok {
		return
	}
	*h = append(*h, Header{key, value})
}

// Update updates the header with the given key and value.
// If the header is not present, it is initialized.
func (h *Headers) Update(key, value string) {
	i, ok := h.Get(key)
	if ok {
		(*h)[i] = Header{key, value}
		return
	}
	*h = append(*h, Header{key, value})
}

// Set sets the headers in the given http.Header.
func (h Headers) Set(header http.Header) {
	for _, x := range h {
		x.Set(header)
	}
}

// Add adds the headers to the given http.Header.
func (h Headers) Add(header http.Header) {
	for _, x := range h {
		x.Add(header)
	}
}

// Header represents a key-value pair.
type Header struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Set sets the header in the given http.Header.
func (h *Header) Set(header http.Header) {
	header.Set(h.Key, h.Value)
}

// Add adds the header to the given http.Header.
func (h *Header) Add(header http.Header) {
	header.Add(h.Key, h.Value)
}
