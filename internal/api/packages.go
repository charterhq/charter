package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/charterhq/charter/internal/pkg"
	"github.com/charterhq/charter/internal/registry"
)

// publish takes a package as tar.gz, checks it like agentctl validate and stores the version.
// Re-publishing the same version with the same content is a no-op; with other content it is refused.
//
//	POST /v1/packages?git_sha=<sha>   body: tar.gz of the package directory
func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	who := caller(r.Context())
	if !s.Publishers[who] {
		writeErr(w, http.StatusForbidden, "caller "+who+" may not publish packages")
		return
	}
	fsys, err := pkg.Unpack(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p, problems := pkg.Load(fsys)
	if problems != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "package has problems", "problems": problems})
		return
	}
	if missing := s.unknownFormats(p); len(missing) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "package has problems", "problems": missing})
		return
	}
	stored, err := pkg.Compile(p)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	src, err := pkg.SourceDigest(fsys)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sha := r.URL.Query().Get("git_sha")
	if sha == "" {
		sha = "api"
	}
	m := p.Agent.Metadata
	id, err := registry.ApplySource(r.Context(), s.Pool, registry.Agent{Name: m.Name, Team: m.Team, Owner: m.Owner}, m.Version, sha, src, stored)
	if errors.Is(err, registry.ErrVersionChanged) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.Log != nil {
		s.Log.Info("package published", "agent", m.Name, "version", m.Version, "version_id", id, "by", who, "git_sha", sha)
	}
	out := map[string]any{"agent": m.Name, "version": m.Version, "version_id": id}
	if len(p.Warnings) > 0 {
		out["warnings"] = p.Warnings
	}
	writeJSON(w, http.StatusOK, out)
}

// unknownFormats: a connection names a webhook format this core has no driver for. The format schema
// only checks the name, the installation decides which formats exist.
func (s *Server) unknownFormats(p *pkg.Package) pkg.Problems {
	var ps pkg.Problems
	for _, c := range p.Connections {
		for src, t := range c.Spec.Triggers {
			if t.Receiver == "" || t.Receiver == "raw" {
				continue
			}
			if _, ok := s.Formats[t.Receiver]; !ok {
				ps = append(ps, pkg.Problem{File: c.File, Msg: "trigger " + src + ": this core has no webhook format " + t.Receiver})
			}
		}
	}
	return ps
}
