package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charterhq/charter/internal/api"
	"github.com/charterhq/charter/internal/pkg"
	"github.com/charterhq/charter/internal/testdb"
)

// copyExample copies the example package so a test can change it.
func copyExample(t *testing.T) string {
	t.Helper()
	src, dst := "../../examples/ticket-triage", t.TempDir()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestPublish(t *testing.T) {
	pool := testdb.New(t)
	s := &api.Server{Pool: pool, Tokens: map[string]string{"ci-token": "gitlab-ci", "cli-token": "lead"},
		Publishers: map[string]bool{"gitlab-ci": true}, Log: slog.Default()}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	post := func(token string, body []byte) (int, map[string]any) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/packages?git_sha=abc", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return resp.StatusCode, out
	}

	dir := copyExample(t)
	archive, err := pkg.Pack(dir)
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := post("cli-token", archive); code != http.StatusForbidden {
		t.Fatalf("non-publisher: %d", code)
	}
	if code, _ := post("ci-token", []byte("not gzip")); code != http.StatusBadRequest {
		t.Fatalf("garbage: %d", code)
	}
	code, out := post("ci-token", archive)
	if code != http.StatusOK || out["agent"] != "ticket-triage" || out["version_id"] == "" {
		t.Fatalf("publish: %d %v", code, out)
	}
	first := out["version_id"]
	if code, out := post("ci-token", archive); code != http.StatusOK || out["version_id"] != first {
		t.Fatalf("same version again: %d %v", code, out)
	}

	// Same version, other content: refused.
	instr := filepath.Join(dir, "scenarios/classify/instructions.md")
	b, _ := os.ReadFile(instr)
	_ = os.WriteFile(instr, append(b, []byte("\nOne more rule.\n")...), 0o644)
	changed, _ := pkg.Pack(dir)
	if code, out := post("ci-token", changed); code != http.StatusConflict || !strings.Contains(out["error"].(string), "bump the version") {
		t.Fatalf("changed version: %d %v", code, out)
	}

	// Broken package: problems come back with files.
	_ = os.Remove(filepath.Join(dir, "scenarios/classify/response.md.tmpl"))
	broken, _ := pkg.Pack(dir)
	code, out = post("ci-token", broken)
	if code != http.StatusUnprocessableEntity || !strings.Contains(strings.ToLower(toJSON(out["problems"])), "response template is missing") {
		t.Fatalf("broken: %d %v", code, out)
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
