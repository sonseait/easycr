package registry

import (
	"context"
	"github.com/distribution/distribution/v3"
	"github.com/distribution/distribution/v3/registry/storage"
	digest "github.com/opencontainers/go-digest"
)

// Distribution reports missing roots on a fresh filesystem as PathNotFound.
// Enumeration of an empty store must succeed without creating layout directories.
type gcNamespace struct{ distribution.Namespace }

func (n gcNamespace) Enumerate(ctx context.Context, fn func(string) error) error {
	err := n.Namespace.(distribution.RepositoryEnumerator).Enumerate(ctx, fn)
	if normalize(err) == ErrNotFound {
		return nil
	}
	return err
}

type gcBlobs struct{ distribution.BlobEnumerator }

func (b gcBlobs) Enumerate(ctx context.Context, fn func(digest.Digest) error) error {
	err := b.BlobEnumerator.Enumerate(ctx, fn)
	if normalize(err) == ErrNotFound {
		return nil
	}
	return err
}
func (n gcNamespace) Blobs() distribution.BlobEnumerator { return gcBlobs{n.Namespace.Blobs()} }

// GC must be called while holding the exclusive metadata lock and no HTTP writer.
func (e *Engine) GC(ctx context.Context, dry bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return storage.MarkAndSweep(ctx, e.Driver, gcNamespace{e.Namespace}, storage.GCOpts{DryRun: dry, RemoveUntagged: true})
}
