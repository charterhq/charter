package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesIndexForUnknownPaths(t *testing.T) {
	h := Handler("/ui")
	for _, p := range []string{"/ui/", "/ui/agents/alert-triage"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", p, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("%s: content type %q", p, ct)
		}
		if rec.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s: index must be revalidated", p)
		}
	}
}

func TestHandlerKeepsTraversalInside(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler("/ui").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/../../go.mod", nil))
	if strings.Contains(rec.Body.String(), "module ") {
		t.Fatal("served a file outside the UI")
	}
}
