package types

import (
	"testing"
	"time"
)

func TestCookieFields(t *testing.T) {
	c := Cookie{
		Name:     "n",
		Value:    "v",
		Domain:   "example.com",
		Expires:  time.Now(),
		HttpOnly: true,
	}
	if c.Name == "" || c.Value == "" || c.Domain == "" {
		t.Fatalf("unexpected cookie: %+v", c)
	}
}
