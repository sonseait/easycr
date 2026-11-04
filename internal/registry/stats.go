package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"easycr/internal/auth"
	"github.com/distribution/distribution/v3"
	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	bolt "go.etcd.io/bbolt"
)

// Activity counts successful manifest GETs, not complete Docker client sessions.
type Activity struct {
	PullCount    uint64     `json:"pullCount"`
	LastPulledAt *time.Time `json:"lastPulledAt"`
	LastPushedAt *time.Time `json:"lastPushedAt"`
}
type RepositoryInfo struct {
	Name       string `json:"name"`
	SizeBytes  int64  `json:"sizeBytes"`
	ImageCount int    `json:"imageCount"`
	Activity
}
type RegistrySummary struct {
	RepositoryCount int   `json:"repositoryCount"`
	ImageCount      int   `json:"imageCount"`
	SizeBytes       int64 `json:"sizeBytes"`
	Activity
}

func activityKey(repo, ref string) []byte { return []byte("activity:" + repo + ":" + ref) }
func (e *Engine) activity(repo, ref string) (Activity, error) {
	var out Activity
	if e.store == nil {
		return out, nil
	}
	err := e.store.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("settings"))
		if b == nil {
			return fmt.Errorf("settings missing")
		}
		if raw := b.Get(activityKey(repo, ref)); raw != nil {
			return json.Unmarshal(raw, &out)
		}
		return nil
	})
	return out, err
}
func (e *Engine) recordActivity(repo, ref string, push bool) error {
	if e.store == nil {
		return nil
	}
	now := time.Now().UTC()
	return e.store.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("settings"))
		if b == nil {
			return fmt.Errorf("settings missing")
		}
		for _, key := range [][]byte{activityKey(repo, ref), activityKey(repo, ""), activityKey("", "")} {
			var a Activity
			if raw := b.Get(key); raw != nil {
				if err := json.Unmarshal(raw, &a); err != nil {
					return err
				}
			}
			if push {
				a.LastPushedAt = &now
			} else {
				a.PullCount++
				a.LastPulledAt = &now
			}
			raw, err := json.Marshal(a)
			if err != nil {
				return err
			}
			if err = b.Put(key, raw); err != nil {
				return err
			}
		}
		return nil
	})
}

type activityWriter struct {
	http.ResponseWriter
	status int
	failed bool
}

func (w *activityWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *activityWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}
func (w *activityWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	n, err := w.ResponseWriter.Write(p)
	if err != nil {
		w.failed = true
	}
	return n, err
}
func (e *Engine) serveTracked(w http.ResponseWriter, r *http.Request) {
	rw := &activityWriter{ResponseWriter: w}
	e.Handler.ServeHTTP(rw, r)
	if rw.failed || (r.Method != http.MethodGet && r.Method != http.MethodPut) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	repo, ref, ok := strings.Cut(path, "/manifests/")
	if !strings.HasPrefix(r.URL.Path, "/v2/") || !ok || !auth.ValidRepository(repo) || ref == "" || strings.Contains(ref, "/") {
		return
	}
	push := r.Method == http.MethodPut
	if (!push && rw.status != 200) || (push && rw.status != 201) {
		return
	}
	d := rw.Header().Get("Docker-Content-Digest")
	if _, err := digest.Parse(d); err != nil {
		return
	}
	if err := e.recordActivity(repo, d, push); err != nil {
		slog.Error("persist registry activity", "error", err)
	}
}

// collectSize traverses indexes and deduplicates the stored compressed blobs.
// It reads manifest JSON only, never layer bodies or external descriptor URLs.
func collectSize(ctx context.Context, r distribution.Repository, d digest.Digest, seen map[digest.Digest]bool, depth int) (int64, error) {
	if seen[d] {
		return 0, nil
	}
	if depth > 32 || len(seen) >= 100000 {
		return 0, fmt.Errorf("manifest graph too large")
	}
	seen[d] = true
	ms, err := r.Manifests(ctx)
	if err != nil {
		return 0, err
	}
	m, err := ms.Get(ctx, d)
	if err != nil {
		return 0, normalize(err)
	}
	_, p, err := m.Payload()
	if err != nil {
		return 0, err
	}
	total := int64(len(p))
	var doc struct {
		Manifests []v1.Descriptor `json:"manifests"`
		Config    v1.Descriptor   `json:"config"`
		Layers    []v1.Descriptor `json:"layers"`
	}
	if err = json.Unmarshal(p, &doc); err != nil {
		return 0, err
	}
	for _, child := range doc.Manifests {
		n, err := collectSize(ctx, r, child.Digest, seen, depth+1)
		if err != nil {
			return 0, err
		}
		total += n
	}
	blobs := append([]v1.Descriptor{}, doc.Layers...)
	if doc.Config.Digest != "" {
		blobs = append(blobs, doc.Config)
	}
	for _, b := range blobs {
		if seen[b.Digest] {
			continue
		}
		seen[b.Digest] = true
		desc, err := r.Blobs(ctx).Stat(ctx, b.Digest)
		if err != nil {
			if len(b.URLs) > 0 {
				continue
			}
			return 0, normalize(err)
		}
		total += desc.Size
	}
	return total, nil
}
func (e *Engine) RepositoryStatistics(ctx context.Context, repo string) (RepositoryInfo, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.repositoryStatistics(ctx, repo, make(map[digest.Digest]bool))
}
func (e *Engine) repositoryStatistics(ctx context.Context, repo string, seen map[digest.Digest]bool) (RepositoryInfo, error) {
	out := RepositoryInfo{Name: repo}
	r, err := e.repository(ctx, repo)
	if err != nil {
		return out, err
	}
	tags, err := r.Tags(ctx).All(ctx)
	if normalize(err) == ErrNotFound {
		tags = nil
		err = nil
	}
	if err != nil {
		return out, err
	}
	images := map[digest.Digest]bool{}
	for _, tag := range tags {
		desc, err := r.Tags(ctx).Get(ctx, tag)
		if err != nil {
			return out, err
		}
		if images[desc.Digest] {
			continue
		}
		images[desc.Digest] = true
		n, err := collectSize(ctx, r, desc.Digest, seen, 0)
		if err != nil {
			return out, err
		}
		out.SizeBytes += n
	}
	out.ImageCount = len(images)
	out.Activity, err = e.activity(repo, "")
	return out, err
}
func (e *Engine) Summary(ctx context.Context) (RegistrySummary, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out RegistrySummary
	names, err := e.names(ctx)
	if err != nil {
		return out, err
	}
	out.RepositoryCount = len(names)
	seen := map[digest.Digest]bool{}
	for _, name := range names {
		info, err := e.repositoryStatistics(ctx, name, seen)
		if err != nil {
			return out, err
		}
		out.ImageCount += info.ImageCount
		out.SizeBytes += info.SizeBytes
	}
	out.Activity, err = e.activity("", "")
	return out, err
}
