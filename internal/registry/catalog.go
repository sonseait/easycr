package registry

import (
	"context"
	"easycr/internal/auth"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/distribution/distribution/v3"
	"github.com/distribution/distribution/v3/registry/storage/driver"
	"github.com/distribution/reference"
	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"io"
	"sort"
	"strings"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")
var ErrInvalid = errors.New("invalid input")

type ManifestInfo struct {
	Name          string          `json:"name"`
	Digest        string          `json:"digest"`
	MediaType     string          `json:"mediaType"`
	ManifestBytes int             `json:"manifestBytes"`
	Platforms     []v1.Platform   `json:"platforms"`
	Manifest      json.RawMessage `json:"manifest,omitempty"`
}

func normalize(err error) error {
	if err == nil {
		return nil
	}
	var a distribution.ErrRepositoryUnknown
	var b distribution.ErrTagUnknown
	var c distribution.ErrManifestUnknownRevision
	var d driver.PathNotFoundError
	if errors.As(err, &a) || errors.As(err, &b) || errors.As(err, &c) || errors.As(err, &d) {
		return ErrNotFound
	}
	return err
}
func (e *Engine) repository(ctx context.Context, name string) (distribution.Repository, error) {
	if !auth.ValidRepository(name) {
		return nil, ErrInvalid
	}
	n, err := reference.WithName(name)
	if err != nil {
		return nil, ErrInvalid
	}
	r, err := e.Namespace.Repository(ctx, n)
	return r, normalize(err)
}
func validTag(tag string) bool {
	n, _ := reference.WithName("placeholder")
	_, err := reference.WithTag(n, tag)
	return err == nil
}
func (e *Engine) names(ctx context.Context) ([]string, error) {
	names := []string{}
	err := e.Namespace.(distribution.RepositoryEnumerator).Enumerate(ctx, func(n string) error { names = append(names, n); return nil })
	if normalize(err) == ErrNotFound {
		err = nil
	}
	sort.Strings(names)
	return names, err
}
func page(names []string, n int, last string) ([]string, string) {
	if n < 1 || n > 100 {
		n = 50
	}
	i := sort.Search(len(names), func(i int) bool { return names[i] > last })
	names = names[i:]
	next := ""
	if len(names) > n {
		names = names[:n]
		next = names[len(names)-1]
	}
	return names, next
}
func (e *Engine) Repositories(ctx context.Context, n int, last string) ([]string, string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	names, err := e.names(ctx)
	if err != nil {
		return nil, "", err
	}
	items, next := page(names, n, last)
	return items, next, nil
}
func (e *Engine) manifest(ctx context.Context, repo, ref string) (ManifestInfo, error) {
	r, err := e.repository(ctx, repo)
	if err != nil {
		return ManifestInfo{}, err
	}
	var d digest.Digest
	if strings.Contains(ref, ":") {
		d, err = digest.Parse(ref)
		if err != nil {
			return ManifestInfo{}, ErrInvalid
		}
	} else {
		if !validTag(ref) {
			return ManifestInfo{}, ErrInvalid
		}
		var desc v1.Descriptor
		desc, err = r.Tags(ctx).Get(ctx, ref)
		d = desc.Digest
	}
	if err != nil {
		return ManifestInfo{}, normalize(err)
	}
	ms, err := r.Manifests(ctx)
	if err != nil {
		return ManifestInfo{}, err
	}
	m, err := ms.Get(ctx, d)
	if err != nil {
		return ManifestInfo{}, normalize(err)
	}
	mt, p, err := m.Payload()
	if err != nil {
		return ManifestInfo{}, err
	}
	info := ManifestInfo{Name: ref, Digest: d.String(), MediaType: mt, ManifestBytes: len(p), Platforms: []v1.Platform{}, Manifest: json.RawMessage(p)}
	var index struct {
		Manifests []v1.Descriptor `json:"manifests"`
		Config    v1.Descriptor   `json:"config"`
	}
	if err = json.Unmarshal(p, &index); err != nil {
		return ManifestInfo{}, err
	}
	for _, desc := range index.Manifests {
		if desc.Platform != nil {
			info.Platforms = append(info.Platforms, *desc.Platform)
		}
	}
	if index.Config.Digest != "" && len(index.Manifests) == 0 {
		reader, err := r.Blobs(ctx).Open(ctx, index.Config.Digest)
		if err != nil {
			return ManifestInfo{}, normalize(err)
		}
		var platform v1.Platform
		err = json.NewDecoder(io.LimitReader(reader, 1<<20)).Decode(&platform)
		_ = reader.Close()
		if err == nil && platform.OS != "" && platform.Architecture != "" {
			info.Platforms = append(info.Platforms, platform)
		}
	}
	return info, nil
}
func (e *Engine) Manifest(ctx context.Context, repo, ref string) (ManifestInfo, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.manifest(ctx, repo, ref)
}
func (e *Engine) Tags(ctx context.Context, repo string, n int, last string) ([]ManifestInfo, string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	r, err := e.repository(ctx, repo)
	if err != nil {
		return nil, "", err
	}
	tags, err := r.Tags(ctx).All(ctx)
	if normalize(err) == ErrNotFound {
		names, listErr := e.names(ctx)
		if listErr != nil {
			return nil, "", listErr
		}
		for _, name := range names {
			if name == repo {
				return []ManifestInfo{}, "", nil
			}
		}
	}
	if err != nil {
		return nil, "", normalize(err)
	}
	sort.Strings(tags)
	tags, next := page(tags, n, last)
	out := []ManifestInfo{}
	for _, tag := range tags {
		m, err := e.manifest(ctx, repo, tag)
		if err != nil {
			return nil, "", err
		}
		m.Manifest = nil
		out = append(out, m)
	}
	return out, next, nil
}
func (e *Engine) Retag(ctx context.Context, repo, tag, source string, overwrite bool) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !validTag(tag) {
		return false, ErrInvalid
	}
	d, err := digest.Parse(source)
	if err != nil {
		return false, ErrInvalid
	}
	r, err := e.repository(ctx, repo)
	if err != nil {
		return false, err
	}
	info, err := e.manifest(ctx, repo, source)
	if err != nil {
		return false, err
	}
	_, err = r.Tags(ctx).Get(ctx, tag)
	created := normalize(err) == ErrNotFound
	if err != nil && !created {
		return false, err
	}
	if !created && !overwrite {
		return false, ErrConflict
	}
	err = r.Tags(ctx).Tag(ctx, tag, v1.Descriptor{Digest: d, MediaType: info.MediaType, Size: int64(info.ManifestBytes)})
	return created, normalize(err)
}
func (e *Engine) Untag(ctx context.Context, repo, tag string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !validTag(tag) {
		return ErrInvalid
	}
	r, err := e.repository(ctx, repo)
	if err != nil {
		return err
	}
	if _, err = r.Tags(ctx).Get(ctx, tag); err != nil {
		return normalize(err)
	}
	return normalize(r.Tags(ctx).Untag(ctx, tag))
}
func (e *Engine) RemoveRepository(ctx context.Context, repo string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !auth.ValidRepository(repo) {
		return ErrInvalid
	}
	names, err := e.names(ctx)
	if err != nil {
		return err
	}
	exists := false
	for _, n := range names {
		if n == repo {
			exists = true
		}
		if strings.HasPrefix(n, repo+"/") {
			return fmt.Errorf("%w: remove nested repositories first", ErrConflict)
		}
	}
	if !exists {
		return ErrNotFound
	}
	n, _ := reference.WithName(repo)
	remover, ok := e.Namespace.(distribution.RepositoryRemover)
	if !ok {
		return fmt.Errorf("repository removal unsupported")
	}
	return normalize(remover.Remove(ctx, n))
}
