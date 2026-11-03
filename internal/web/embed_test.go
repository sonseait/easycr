package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbedded(t *testing.T) {
	for path, mt := range map[string]string{"/": "text/html", "/app.js": "text/javascript", "/style.css": "text/css"} {
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), mt) || w.Body.Len() == 0 {
			t.Fatal(path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest("GET", "/private", nil))
	if w.Code != 404 {
		t.Fatal("unknown asset returned shell")
	}
}
