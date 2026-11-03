package httpapi

import (
	"easycr/internal/auth"
	"easycr/internal/registry"
	"errors"
	"net/http"
	"strconv"
)

func imageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, registry.ErrNotFound):
		failure(w, 404, "not_found", "Repository, tag, or manifest does not exist")
	case errors.Is(err, registry.ErrConflict):
		failure(w, 409, "conflict", "Tag exists, or repository has nested repositories; confirm overwrite or remove descendants first")
	case errors.Is(err, registry.ErrInvalid):
		failure(w, 400, "invalid_input", "Invalid repository, tag, or digest")
	default:
		failure(w, 500, "internal_error", "Registry operation failed")
	}
}
func paging(w http.ResponseWriter, r *http.Request) (int, string, bool) {
	n := 50
	var err error
	if v := r.URL.Query().Get("n"); v != "" {
		n, err = strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			failure(w, 400, "invalid_paging", "n must be between 1 and 100")
			return 0, "", false
		}
	}
	return n, r.URL.Query().Get("last"), true
}
func repository(w http.ResponseWriter, r *http.Request) (string, bool) {
	repo := r.URL.Query().Get("repository")
	if !auth.ValidRepository(repo) {
		failure(w, 400, "invalid_repository", "Provide an exact lowercase repository name")
		return "", false
	}
	return repo, true
}
func (a *server) repositories(w http.ResponseWriter, r *http.Request) {
	n, last, ok := paging(w, r)
	if !ok {
		return
	}
	items, next, err := a.engine.Repositories(r.Context(), n, last)
	if err != nil {
		imageError(w, err)
		return
	}
	out := []map[string]string{}
	for _, name := range items {
		out = append(out, map[string]string{"name": name})
	}
	reply(w, 200, map[string]any{"items": out, "next": next})
}
func (a *server) tags(w http.ResponseWriter, r *http.Request) {
	repo, ok := repository(w, r)
	if !ok {
		return
	}
	n, last, ok := paging(w, r)
	if !ok {
		return
	}
	items, next, err := a.engine.Tags(r.Context(), repo, n, last)
	if err != nil {
		imageError(w, err)
		return
	}
	reply(w, 200, map[string]any{"items": items, "next": next})
}
func (a *server) manifest(w http.ResponseWriter, r *http.Request) {
	repo, ok := repository(w, r)
	if !ok {
		return
	}
	info, err := a.engine.Manifest(r.Context(), repo, r.URL.Query().Get("reference"))
	if err != nil {
		imageError(w, err)
		return
	}
	reply(w, 200, info)
}
func (a *server) retag(w http.ResponseWriter, r *http.Request) {
	repo, ok := repository(w, r)
	if !ok {
		return
	}
	var in struct {
		SourceDigest string `json:"sourceDigest"`
		Overwrite    bool   `json:"overwrite"`
	}
	if !decode(w, r, &in) {
		return
	}
	created, err := a.engine.Retag(r.Context(), repo, r.URL.Query().Get("tag"), in.SourceDigest, in.Overwrite)
	if err != nil {
		imageError(w, err)
		return
	}
	status := 200
	if created {
		status = 201
	}
	reply(w, status, map[string]bool{"created": created})
}
func (a *server) untag(w http.ResponseWriter, r *http.Request) {
	repo, ok := repository(w, r)
	if !ok {
		return
	}
	if err := a.engine.Untag(r.Context(), repo, r.URL.Query().Get("tag")); err != nil {
		imageError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (a *server) removeRepository(w http.ResponseWriter, r *http.Request) {
	repo, ok := repository(w, r)
	if !ok {
		return
	}
	if err := a.engine.RemoveRepository(r.Context(), repo); err != nil {
		imageError(w, err)
		return
	}
	w.WriteHeader(204)
}
