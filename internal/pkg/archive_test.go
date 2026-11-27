package pkg

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestBriefIsNotPartOfVersion(t *testing.T) {
	files := map[string]string{"agent.yml": "kind: Agent\n", "persona.md": "role\n", "knowledge/brief.md": "a knowledge file\n"}
	without := fstest.MapFS{}
	for name, body := range files {
		without[name] = &fstest.MapFile{Data: []byte(body)}
	}
	with := fstest.MapFS{Brief: &fstest.MapFile{Data: []byte("# why\n")}}
	for name, f := range without {
		with[name] = f
	}
	a, err := SourceDigest(without)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SourceDigest(with)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("brief.md changed the source digest")
	}

	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, Brief), []byte("# why\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive, err := Pack(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unpack(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(got, Brief); err == nil {
		t.Fatal("brief.md went into the package archive")
	}
	if _, err := fs.Stat(got, "knowledge/brief.md"); err != nil {
		t.Fatal("a file named brief.md outside the package root must stay in the archive")
	}
	if c, _ := SourceDigest(got); c != a {
		t.Fatal("the unpacked archive has another source digest")
	}
}
