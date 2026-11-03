package registry

import (
	"net/http/httptest"
	"testing"
)

func TestEngineV2(t *testing.T) {
	h, err := NewEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/v2/", nil))
	if w.Code != 200 || w.Header().Get("Docker-Distribution-Api-Version") != "registry/2.0" {
		t.Fatalf("status %d headers %v", w.Code, w.Header())
	}
}
