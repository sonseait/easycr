package registry

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRepositoryIncludesNamedTags(t *testing.T) {
	e, _ := fixture(t)
	image(t, e, "team/app", "latest", "amd64", []byte("layer"))
	image(t, e, "team/app", "v1.0", "amd64", []byte("layer"))
	image(t, e, "team/app", "sha256-deadbeef", "amd64", []byte("layer"))
	info, err := e.RepositoryStatistics(context.Background(), "team/app")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Tags []string `json:"tags"`
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Tags) != 2 || out.Tags[0] != "latest" || out.Tags[1] != "v1.0" {
		t.Fatalf("named tags: %s", raw)
	}
}
