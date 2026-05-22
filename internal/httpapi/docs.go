package httpapi

import (
	_ "embed"
	"net/http"
)

//go:embed openapi/openapi.yaml
var openAPISpec []byte

//go:embed static/docs.html
var docsHTML []byte

func (s *Server) serveOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openAPISpec)
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
