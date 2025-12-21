package common

import (
	"encoding/json"
	"testing"

	"github.com/warpdl/warpdl/pkg/warplib"
)

func TestDownloadParamsJSON(t *testing.T) {
	p := DownloadParams{
		Url:        "http://example.com",
		FileName:   "file.bin",
		Headers:    warplib.Headers{{Key: warplib.USER_AGENT_KEY, Value: "ua"}},
		ForceParts: true,
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out DownloadParams
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Url != p.Url || out.FileName != p.FileName {
		t.Fatalf("unexpected round trip: %+v", out)
	}
}
