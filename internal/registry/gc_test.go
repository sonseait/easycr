package registry

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"github.com/distribution/distribution/v3"
	"github.com/distribution/reference"
	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func snapshot(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	if err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			out[p] = sha256.Sum256(b)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestGCReachability(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	e, err := OpenStorage(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	a := image(t, e, "multi/app", "", "amd64", []byte("shared"))
	b := image(t, e, "multi/app", "", "arm64", []byte("shared"))
	orphan := image(t, e, "multi/app", "gone", "s390x", []byte("orphan"))
	image(t, e, "other/app", "live", "amd64", []byte("shared"))
	named, _ := reference.WithName("multi/app")
	r, _ := e.Namespace.Repository(ctx, named)
	ms, _ := r.Manifests(ctx)
	descs := []v1.Descriptor{}
	for i, d := range []digest.Digest{a, b} {
		m, err := ms.Get(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		mt, p, _ := m.Payload()
		descs = append(descs, v1.Descriptor{Digest: d, MediaType: mt, Size: int64(len(p)), Platform: &v1.Platform{OS: "linux", Architecture: []string{"amd64", "arm64"}[i]}})
	}
	payload, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": v1.MediaTypeImageIndex, "manifests": descs})
	m, _, err := distribution.UnmarshalManifest(v1.MediaTypeImageIndex, payload)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := ms.Put(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Tags(ctx).Tag(ctx, "latest", v1.Descriptor{Digest: idx}); err != nil {
		t.Fatal(err)
	}
	if err = e.Untag(ctx, "multi/app", "gone"); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, dir)
	if err = e.GC(ctx, true); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot(t, dir)) {
		t.Fatal("dry-run mutated files")
	}
	if err = e.GC(ctx, false); err != nil {
		t.Fatal(err)
	}
	for _, d := range []digest.Digest{a, b, idx} {
		info, err := e.Manifest(ctx, "multi/app", d.String())
		if err != nil {
			t.Fatal("live manifest lost", d, err)
		}
		if d == idx && string(info.Manifest) != string(payload) {
			t.Fatal("index bytes changed")
		}
	}
	if _, err = e.Manifest(ctx, "multi/app", orphan.String()); err != ErrNotFound {
		t.Fatal("orphan not reclaimed", err)
	}
	if _, err = e.Namespace.BlobStatter().Stat(ctx, orphan); err == nil {
		t.Fatal("orphan global blob remains")
	}
	if _, err = e.Manifest(ctx, "other/app", "live"); err != nil {
		t.Fatal("shared image lost", err)
	}
	for _, d := range descs {
		child, err := ms.Get(ctx, d.Digest)
		if err != nil {
			t.Fatal(err)
		}
		for _, blob := range child.References() {
			if _, err = r.Blobs(ctx).Stat(ctx, blob.Digest); err != nil {
				t.Fatal("child blob/link lost", err)
			}
		}
	}
}
func TestRemoveRepositoryNestedGuard(t *testing.T) {
	e, _ := fixture(t)
	ctx := context.Background()
	image(t, e, "team/app", "v1", "amd64", []byte("a"))
	image(t, e, "team/app/child", "v1", "amd64", []byte("a"))
	if err := e.RemoveRepository(ctx, "team/app"); err == nil {
		t.Fatal("nested repo removal was allowed")
	}
	if _, err := e.Manifest(ctx, "team/app/child", "v1"); err != nil {
		t.Fatal(err)
	}
}
