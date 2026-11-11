package registry

import (
	"context"
	"encoding/json"
	"github.com/distribution/distribution/v3"
	"github.com/distribution/reference"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"testing"
)

func TestIndexLayersAndDeduplicatedSize(t *testing.T) {
	e, _ := fixture(t)
	ctx := context.Background()
	d := image(t, e, "team/app", "child", "amd64", []byte("layer"))
	child, err := e.Manifest(ctx, "team/app", "child")
	if err != nil {
		t.Fatal(err)
	}
	named, _ := reference.WithName("team/app")
	r, err := e.Namespace.Repository(ctx, named)
	if err != nil {
		t.Fatal(err)
	}
	desc := v1.Descriptor{Digest: d, Size: int64(child.ManifestBytes), MediaType: v1.MediaTypeImageManifest, Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}}
	payload, err := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": v1.MediaTypeImageIndex, "manifests": []v1.Descriptor{desc, desc}})
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := distribution.UnmarshalManifest(v1.MediaTypeImageIndex, payload)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := r.Manifests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := ms.Put(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Tags(ctx).Tag(ctx, "multi", v1.Descriptor{Digest: idx}); err != nil {
		t.Fatal(err)
	}
	info, err := e.Manifest(ctx, "team/app", "multi")
	if err != nil {
		t.Fatal(err)
	}
	if info.SizeBytes != child.SizeBytes+int64(len(payload)) || len(info.Children) != 2 || len(info.Layers) != 0 {
		t.Fatalf("index: %+v", info)
	}
}
