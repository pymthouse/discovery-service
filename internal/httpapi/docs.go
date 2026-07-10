package httpapi

import (
	"bytes"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	// Import embed so the //go:embed directives below are compiled.
	_ "embed"
)

//go:embed openapi/openapi.yaml
var openAPISpec []byte

//go:embed static/docs.html
var docsHTML []byte

// openAPIServersBlock matches the top-level OpenAPI `servers:` list so it can
// be replaced with the trusted public base URL from config.
var openAPIServersBlock = regexp.MustCompile(`(?m)^servers:\n(?:[ \t]+.*\n)*`)

func (s *Server) serveOpenAPI(w http.ResponseWriter, r *http.Request) {
	spec := openAPISpec
	if base := strings.TrimSpace(s.cfg.PublicBaseURL); base != "" {
		spec = rewriteOpenAPIServers(spec, base)
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(spec)
}

func (s *Server) serveDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(docsHTML)
}

func (s *Server) redirectHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/docs", http.StatusFound)
}

// rewriteOpenAPIServers replaces the embedded servers list with a single entry
// pointing at baseURL. baseURL must already be validated by config (never taken
// from request headers).
func rewriteOpenAPIServers(spec []byte, baseURL string) []byte {
	block := fmt.Sprintf(
		"servers:\n  - url: %q\n    description: Current deployment\n",
		baseURL,
	)
	if openAPIServersBlock.Match(spec) {
		return openAPIServersBlock.ReplaceAll(spec, []byte(block))
	}
	// Spec missing a servers block — insert after the info section's trailing newline
	// by prepending after the first document key if needed.
	if idx := bytes.Index(spec, []byte("\npaths:")); idx >= 0 {
		out := make([]byte, 0, len(spec)+len(block)+1)
		out = append(out, spec[:idx+1]...)
		out = append(out, block...)
		out = append(out, spec[idx+1:]...)
		return out
	}
	return append(append([]byte{}, block...), spec...)
}
