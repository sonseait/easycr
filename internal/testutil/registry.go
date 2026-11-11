// Package testutil drives the real Distribution HTTP handler without a mock store.
package testutil

import (
	"bytes"
	"encoding/json"
	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func Request(t testing.TB, h http.Handler, method, path, secret string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	if secret != "" {
		r.SetBasicAuth("token", secret)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func Upload(t testing.TB, h http.Handler, repo, secret string, b []byte, mt string) v1.Descriptor {
	t.Helper()
	w := Request(t, h, "POST", "/v2/"+repo+"/blobs/uploads/", secret, nil, nil)
	if w.Code != 202 {
		t.Fatalf("start blob upload: status %d", w.Code)
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	d := digest.FromBytes(b)
	q := u.Query()
	q.Set("digest", d.String())
	u.RawQuery = q.Encode()
	w = Request(t, h, "PUT", u.String(), secret, b, map[string]string{"Content-Type": "application/octet-stream"})
	if w.Code != 201 {
		t.Fatalf("complete blob upload: status %d", w.Code)
	}
	return v1.Descriptor{Digest: d, Size: int64(len(b)), MediaType: mt}
}
func PushImage(t testing.TB, h http.Handler, repo, tag, secret string) (digest.Digest, []byte, []v1.Descriptor) {
	t.Helper()
	config := []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}`)
	c := Upload(t, h, repo, secret, config, v1.MediaTypeImageConfig)
	l := Upload(t, h, repo, secret, []byte("fixture-layer"), v1.MediaTypeImageLayer)
	payload, err := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": v1.MediaTypeImageManifest, "config": c, "layers": []v1.Descriptor{l}})
	if err != nil {
		t.Fatal(err)
	}
	d := digest.FromBytes(payload)
	w := Request(t, h, "PUT", "/v2/"+repo+"/manifests/"+tag, secret, payload, map[string]string{"Content-Type": v1.MediaTypeImageManifest})
	if w.Code != 201 {
		t.Fatalf("push manifest status %d: %s", w.Code, w.Body.String())
	}
	return d, payload, []v1.Descriptor{c, l}
}
func ReadBody(t testing.TB, r *http.Response) []byte {
	t.Helper()
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
