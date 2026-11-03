package registry

import (
	"bytes"
	"context"
	"easycr/internal/auth"
	"easycr/internal/state"
	"easycr/internal/testutil"
	"encoding/json"
	"fmt"
	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Engine, auth.Service) {
	t.Helper()
	s, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err = auth.EnsureBootstrap(s, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	svc := auth.Service{Store: s}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e, err := NewEngine(ctx, t.TempDir(), svc)
	if err != nil {
		t.Fatal(err)
	}
	return e, svc
}
func TestUploadURLSurvivesEngineRestart(t *testing.T) {
	stateDir, storageDir := t.TempDir(), t.TempDir()
	s, err := state.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.EnsureBootstrap(s, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	svc := auth.Service{Store: s}
	_, token, err := svc.CreateToken("push", []auth.Grant{{Repository: "team/app", Permission: "write"}})
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewEngine(context.Background(), storageDir, svc)
	if err != nil {
		t.Fatal(err)
	}
	w := testutil.Request(t, e, "POST", "/v2/team/app/blobs/uploads/", token, nil, nil)
	if w.Code != 202 {
		t.Fatalf("start upload: %d %s", w.Code, w.Body.String())
	}
	location := w.Header().Get("Location")
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = state.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, err = NewEngine(context.Background(), storageDir, auth.Service{Store: s})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	w = testutil.Request(t, e, "PATCH", location, token, []byte("resumed"), map[string]string{"Content-Type": "application/octet-stream"})
	if w.Code != 202 {
		t.Fatalf("resume after restart: %d %s", w.Code, w.Body.String())
	}
}

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	read, write time.Time
}

func (w *deadlineRecorder) SetReadDeadline(t time.Time) error {
	if !t.IsZero() {
		w.read = t
	}
	return nil
}
func (w *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	if !t.IsZero() {
		w.write = t
	}
	return nil
}

func TestEngineBoundsStreamDuration(t *testing.T) {
	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	e := &Engine{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})}
	start := time.Now()
	e.ServeHTTP(w, httptest.NewRequest("PUT", "/v2/a/blobs/uploads/", nil))
	if w.read.IsZero() || w.write.IsZero() || !w.read.After(start) || !w.write.After(start) {
		t.Fatalf("stream deadlines not set: read=%v write=%v", w.read, w.write)
	}
	if w.read.Sub(start) > time.Hour+time.Second || w.write.Sub(start) > time.Hour+time.Second {
		t.Fatalf("stream deadlines are not bounded: read=%v write=%v", w.read, w.write)
	}
}

func TestStalledUploadReleasesMutationLock(t *testing.T) {
	started := make(chan struct{})
	e := &Engine{streamTimeout: 100 * time.Millisecond, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			close(started)
			_, _ = io.Copy(io.Discard, r.Body)
		}
		w.WriteHeader(http.StatusNoContent)
	})}
	s := httptest.NewServer(e)
	defer s.Close()
	c, err := net.Dial("tcp", s.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprint(c, "PUT /v2/a HTTP/1.1\r\nHost: example.com\r\nContent-Length: 100\r\n\r\nx")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upload did not start")
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Post(s.URL+"/v2/b", "text/plain", nil)
	if err != nil {
		t.Fatalf("other request remained blocked: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", response.StatusCode)
	}
}

func TestEngineV2(t *testing.T) {
	e, s := fixture(t)
	for _, credential := range []bool{false, true} {
		r := httptest.NewRequest("GET", "/v2/", nil)
		want := 401
		if credential {
			_, secret, err := s.CreateToken("test", []auth.Grant{{Repository: "a", Permission: "read"}})
			if err != nil {
				t.Fatal(err)
			}
			r.SetBasicAuth("token", secret)
			want = 200
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		if w.Code != want || w.Header().Get("Docker-Distribution-Api-Version") != "registry/2.0" {
			t.Fatalf("status %d want %d", w.Code, want)
		}
		if !credential && w.Header().Get("WWW-Authenticate") == "" {
			t.Fatal("challenge missing")
		}
	}
}
func TestRegistryAuthorization(t *testing.T) {
	e, s := fixture(t)
	for _, g := range []auth.Grant{{Repository: "team/app", Permission: "read"}, {Repository: "team/app", Permission: "write"}, {Repository: "*", Permission: "read"}, {Repository: "*", Permission: "write"}} {
		rec, secret, err := s.CreateToken("test", []auth.Grant{g})
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			method, path string
			allow        bool
		}{{"GET", "/v2/_catalog", g.Repository == "*"}, {"POST", "/v2/team/app/blobs/uploads/", g.Permission == "write"}, {"GET", "/v2/foreign/app/tags/list", g.Repository == "*"}, {"DELETE", "/v2/team/app/manifests/sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false}} {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r.SetBasicAuth("token", secret)
			w := httptest.NewRecorder()
			e.ServeHTTP(w, r)
			denied := w.Code == 401 || w.Code == 403
			if denied == tc.allow {
				t.Errorf("grant=%v %s %s status=%d", g, tc.method, tc.path, w.Code)
			}
		}
		if err = s.Revoke(rec.ID); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("GET", "/v2/", nil)
		r.SetBasicAuth("token", secret)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("revocation ignored")
		}
	}
}

func TestEngineRoundTrip(t *testing.T) {
	e, s := fixture(t)
	_, secret, err := s.CreateToken("ci", []auth.Grant{{Repository: "team/app", Permission: "write"}})
	if err != nil {
		t.Fatal(err)
	}
	d, p, blobs := testutil.PushImage(t, e, "team/app", "v1", secret)
	for _, ref := range []string{"v1", d.String()} {
		w := testutil.Request(t, e, "GET", "/v2/team/app/manifests/"+ref, secret, nil, map[string]string{"Accept": v1.MediaTypeImageManifest})
		if w.Code != 200 || !bytes.Equal(p, w.Body.Bytes()) || w.Header().Get("Docker-Content-Digest") != d.String() {
			t.Fatal("manifest round-trip", w.Code)
		}
	}
	for _, blob := range blobs {
		w := testutil.Request(t, e, "GET", "/v2/team/app/blobs/"+blob.Digest.String(), secret, nil, nil)
		if w.Code != 200 || digest.FromBytes(w.Body.Bytes()) != blob.Digest {
			t.Fatal("blob round-trip", w.Code)
		}
	}
}
func TestCrossRepoMount(t *testing.T) {
	e, s := fixture(t)
	_, source, err := s.CreateToken("source", []auth.Grant{{Repository: "source/app", Permission: "write"}})
	if err != nil {
		t.Fatal(err)
	}
	blob := testutil.Upload(t, e, "source/app", source, []byte("mount fixture"), v1.MediaTypeImageLayer)
	for _, canRead := range []bool{false, true} {
		grants := []auth.Grant{{Repository: "dest/app", Permission: "write"}}
		if canRead {
			grants = append(grants, auth.Grant{Repository: "source/app", Permission: "read"})
		}
		_, secret, err := s.CreateToken("dest", grants)
		if err != nil {
			t.Fatal(err)
		}
		path := "/v2/dest/app/blobs/uploads/?mount=" + url.QueryEscape(blob.Digest.String()) + "&from=source/app"
		w := testutil.Request(t, e, "POST", path, secret, nil, nil)
		if canRead && w.Code != 201 {
			t.Fatal("authorized mount denied", w.Code)
		}
		if !canRead && w.Code != 401 && w.Code != 403 {
			t.Fatal("source denied mount not rejected", w.Code)
		}
	}
}

func TestReadTokenAndDockerManifest(t *testing.T) {
	e, s := fixture(t)
	_, writer, err := s.CreateToken("writer", []auth.Grant{{Repository: "team/app", Permission: "write"}})
	if err != nil {
		t.Fatal(err)
	}
	_, payload, blobs := testutil.PushImage(t, e, "team/app", "oci", writer)
	var docker map[string]any
	if err = json.Unmarshal(payload, &docker); err != nil {
		t.Fatal(err)
	}
	mt := "application/vnd.docker.distribution.manifest.v2+json"
	docker["mediaType"] = mt
	docker["config"].(map[string]any)["mediaType"] = "application/vnd.docker.container.image.v1+json"
	for _, l := range docker["layers"].([]any) {
		l.(map[string]any)["mediaType"] = "application/vnd.docker.image.rootfs.diff.tar.gzip"
	}
	payload, _ = json.Marshal(docker)
	w := testutil.Request(t, e, "PUT", "/v2/team/app/manifests/docker", writer, payload, map[string]string{"Content-Type": mt})
	if w.Code != 201 {
		t.Fatal("Docker schema2 push", w.Code)
	}
	_, reader, err := s.CreateToken("reader", []auth.Grant{{Repository: "team/app", Permission: "read"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "HEAD"} {
		w = testutil.Request(t, e, method, "/v2/team/app/manifests/docker", reader, nil, map[string]string{"Accept": mt})
		if w.Code != 200 || w.Header().Get("Content-Type") != mt {
			t.Fatal("Docker read", method, w.Code)
		}
		if method == "GET" && !bytes.Equal(w.Body.Bytes(), payload) {
			t.Fatal("Docker manifest bytes changed")
		}
	}
	w = testutil.Request(t, e, "GET", "/v2/team/app/blobs/"+blobs[0].Digest.String(), reader, nil, nil)
	if w.Code != 200 {
		t.Fatal("read blob denied")
	}
	for _, path := range []string{"/v2/team/app2/tags/list", "/v2/team/app/sub/tags/list"} {
		w = testutil.Request(t, e, "GET", path, reader, nil, nil)
		if w.Code != 401 {
			t.Fatal("prefix/descendant read allowed")
		}
	}
	w = testutil.Request(t, e, "PUT", "/v2/team/app/manifests/forbidden", reader, payload, map[string]string{"Content-Type": mt})
	if w.Code != 401 {
		t.Fatal("read token pushed manifest")
	}
}
