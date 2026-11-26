package pkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing/fstest"
)

// Limits of a package archive: what a package of text files and schemas needs, with room.
const (
	maxArchiveFiles = 2000
	maxArchiveBytes = 32 << 20
)

// Pack makes a tar.gz of a package directory: regular files only, paths relative to the directory.
func Pack(dir string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && p != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		b, err := readFile(p)
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel), Mode: 0o644, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err = tw.Write(b)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Unpack reads a package tar.gz into memory. Links, absolute paths and paths leaving the package are refused.
func Unpack(r io.Reader) (fs.FS, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("package archive is not gzip: %w", err)
	}
	tr := tar.NewReader(gz)
	out := fstest.MapFS{}
	total := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("package archive: %w", err)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
		default:
			return nil, fmt.Errorf("package archive: %s is not a regular file", h.Name)
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "./"))
		if !fs.ValidPath(name) || name == "." {
			return nil, fmt.Errorf("package archive: bad path %q", h.Name)
		}
		if len(out) >= maxArchiveFiles {
			return nil, fmt.Errorf("package archive: more than %d files", maxArchiveFiles)
		}
		b, err := io.ReadAll(io.LimitReader(tr, int64(maxArchiveBytes-total)+1))
		if err != nil {
			return nil, fmt.Errorf("package archive: %w", err)
		}
		total += len(b)
		if total > maxArchiveBytes {
			return nil, fmt.Errorf("package archive: more than %d bytes", maxArchiveBytes)
		}
		out[name] = &fstest.MapFile{Data: b, Mode: 0o644}
	}
	if len(out) == 0 {
		return nil, errors.New("package archive is empty")
	}
	return out, nil
}

func readFile(p string) ([]byte, error) { return os.ReadFile(p) }

// SourceDigest is the SHA-256 of the package files as Pack would take them: paths and contents in
// path order. It identifies a version independently of how a core compiles it.
func SourceDigest(fsys fs.FS) (string, error) {
	var files []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && !strings.HasPrefix(d.Name(), ".") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", f, len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
