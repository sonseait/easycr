package httpapi

import (
	"easycr/internal/auth"
	"easycr/internal/testutil"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestImageAPI(t *testing.T) {
	c, s := setup(t, false)
	c.login(t)
	_, secret, err := s.CreateToken("writer", []auth.Grant{{Repository: "*", Permission: "write"}})
	if err != nil {
		t.Fatal(err)
	}
	d, p, _ := testutil.PushImage(t, c.h, "team/app", "v1", secret)
	testutil.PushImage(t, c.h, "other/app", "v1", secret)
	w := c.request("GET", "/api/repositories?n=1", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"next":"other/app"`) {
		t.Fatal("catalog page", w.Code)
	}
	w = c.request("GET", "/api/repositories?n=1&last=other%2Fapp", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "team/app") {
		t.Fatal("catalog cursor", w.Code)
	}
	w = c.request("GET", "/api/tags?repository=team%2Fapp", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), d.String()) || !strings.Contains(w.Body.String(), `"manifestBytes":`) {
		t.Fatal("tags", w.Code)
	}
	w = c.request("GET", "/api/manifest?repository=team%2Fapp&reference=v1", "", nil)
	var info struct {
		Manifest json.RawMessage `json:"manifest"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || string(info.Manifest) != string(p) {
		t.Fatal("manifest bytes", w.Code)
	}
	body := `{"sourceDigest":"` + d.String() + `","overwrite":false}`
	for i := 0; i < 2; i++ {
		w = c.request("PUT", "/api/tag?repository=team%2Fapp&tag=alias", body, nil)
		want := 201
		if i == 1 {
			want = 409
		}
		if w.Code != want {
			t.Fatal("retag", w.Code, want)
		}
	}
	w = c.request("PUT", "/api/tag?repository=team%2Fapp&tag=alias", strings.Replace(body, "false", "true", 1), nil)
	if w.Code != 200 {
		t.Fatal("overwrite", w.Code)
	}
	if w = c.request("DELETE", "/api/tag?repository=team%2Fapp&tag=v1", "", nil); w.Code != 204 {
		t.Fatal("untag", w.Code)
	}
	if w = c.request("GET", "/api/manifest?repository=team%2Fapp&reference=alias", "", nil); w.Code != 200 {
		t.Fatal("alias removed", w.Code)
	}
	if w = c.request("DELETE", "/api/repository?repository=team%2Fapp", "", nil); w.Code != 204 {
		t.Fatal("repo deletion", w.Code)
	}
	if w = c.request("GET", "/api/manifest?repository=other%2Fapp&reference=v1", "", nil); w.Code != 200 {
		t.Fatal("shared repo lost")
	}
	for _, path := range []string{"/api/repositories?n=101", "/api/repositories?n=no", "/api/tags?repository=" + url.QueryEscape("../bad"), "/api/manifest?repository=team%2Fapp&reference=bad:hash"} {
		if w = c.request("GET", path, "", nil); w.Code != 400 {
			t.Fatal("invalid input", path, w.Code)
		}
	}
	if w = c.request("GET", "/api/tags?repository=missing%2Frepo", "", nil); w.Code != 404 {
		t.Fatal("missing repo", w.Code)
	}
}
