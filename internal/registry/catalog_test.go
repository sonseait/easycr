package registry

import (
	"context"
	"encoding/json"
	"github.com/distribution/distribution/v3"
	"github.com/distribution/reference"
	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"testing"
)

func image(t *testing.T, e *Engine, repo, tag, arch string, layer []byte) digest.Digest {
	t.Helper()
	ctx := context.Background()
	named, err := reference.WithName(repo)
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Namespace.Repository(ctx, named)
	if err != nil {
		t.Fatal(err)
	}
	config := []byte(`{"architecture":"` + arch + `","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}`)
	c, err := r.Blobs(ctx).Put(ctx, v1.MediaTypeImageConfig, config)
	if err != nil {
		t.Fatal(err)
	}
	l, err := r.Blobs(ctx).Put(ctx, v1.MediaTypeImageLayer, layer)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": v1.MediaTypeImageManifest, "config": c, "layers": []v1.Descriptor{l}})
	m, _, err := distribution.UnmarshalManifest(v1.MediaTypeImageManifest, payload)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := r.Manifests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := ms.Put(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "" {
		err = r.Tags(ctx).Tag(ctx, tag, v1.Descriptor{Digest: d})
		if err != nil {
			t.Fatal(err)
		}
	}
	return d
}
func TestCatalog(t *testing.T) {
	e, _ := fixture(t)
	ctx := context.Background()
	repos, next, err := e.Repositories(ctx, 1, "")
	if err != nil || len(repos) != 0 || next != "" {
		t.Fatal("empty catalog", repos, next, err)
	}
	image(t, e, "team/app", "v1", "amd64", []byte("a"))
	image(t, e, "other/app", "v1", "amd64", []byte("a"))
	repos, next, err = e.Repositories(ctx, 1, "")
	if err != nil || len(repos) != 1 || repos[0] != "other/app" || next != "other/app" {
		t.Fatal("first page", repos, next, err)
	}
	repos, next, err = e.Repositories(ctx, 1, next)
	if err != nil || len(repos) != 1 || repos[0] != "team/app" || next != "" {
		t.Fatal("last page", repos, next, err)
	}
}
func TestRetagAndUntag(t *testing.T) {
	e, _ := fixture(t)
	ctx := context.Background()
	d := image(t, e, "team/app", "a", "amd64", []byte("a"))
	created, err := e.Retag(ctx, "team/app", "b", d.String(), false)
	if err != nil || !created {
		t.Fatal("retag", err)
	}
	if _, err = e.Retag(ctx, "team/app", "b", d.String(), false); err != ErrConflict {
		t.Fatal("expected conflict", err)
	}
	if err = e.Untag(ctx, "team/app", "a"); err != nil {
		t.Fatal(err)
	}
	info, err := e.Manifest(ctx, "team/app", "b")
	if err != nil || info.Digest != d.String() || info.ManifestBytes == 0 {
		t.Fatal("alias damaged", err)
	}
	if _, err = e.Manifest(ctx, "team/app", d.String()); err != nil {
		t.Fatal("digest damaged", err)
	}
	tags, _, err := e.Tags(ctx, "team/app", 50, "")
	if err != nil || len(tags) != 1 || tags[0].Name != "b" {
		t.Fatal(tags, err)
	}
	if err = e.Untag(ctx, "../bad", "b"); err == nil {
		t.Fatal("invalid repo accepted")
	}
}
func TestRemoveRepository(t *testing.T) {
	e, _ := fixture(t)
	ctx := context.Background()
	image(t, e, "team/app", "v1", "amd64", []byte("shared"))
	image(t, e, "other/app", "v1", "amd64", []byte("shared"))
	if err := e.RemoveRepository(ctx, "team/app"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Manifest(ctx, "other/app", "v1"); err != nil {
		t.Fatal("shared image damaged", err)
	}
	if err := e.RemoveRepository(ctx, "team/app"); err != ErrNotFound {
		t.Fatal("expected not found", err)
	}
}

func TestTaglessRepositoryAndPlatformMetadata(t *testing.T) {
	e, _ := fixture(t)
	ctx := context.Background()
	d := image(t, e, "team/tagless", "", "arm64", []byte("a"))
	items, _, err := e.Tags(ctx, "team/tagless", 50, "")
	if err != nil || len(items) != 0 {
		t.Fatal("existing tagless repo must return empty list", err)
	}
	info, err := e.Manifest(ctx, "team/tagless", d.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Platforms) != 1 || info.Platforms[0].Architecture != "arm64" || info.Platforms[0].OS != "linux" {
		t.Fatal("single-image platform metadata missing")
	}
}
