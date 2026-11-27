// Package webui serves the web UI built from web/ and embedded into agentd.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// The build of web/ lands in dist (npm run build); without it the UI says how to build it.
//
//go:embed all:dist
var dist embed.FS

const notBuilt = `<!doctype html><meta charset="utf-8"><title>Charter</title>
<p style="font:14px system-ui;padding:2rem">The web UI is not built into this agentd: run <code>npm ci &amp;&amp; npm run build</code> in <code>web/</code> and rebuild.</p>`

// Handler serves the UI under prefix. Unknown paths get index.html: the UI routes on its own.
// Assets carry content hashes and are cached for good; index.html is always revalidated.
func Handler(prefix string) http.Handler {
	root, _ := fs.Sub(dist, "dist")
	files := http.FileServerFS(root)
	return http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if st, err := fs.Stat(root, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(root, "index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if err != nil {
			_, _ = w.Write([]byte(notBuilt))
			return
		}
		_, _ = w.Write(index)
	}))
}
