package server

import (
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) handleDebugPathQuery(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "path=%s query=%s", trimDebugPrefix(r.URL.Path, "/_gnar/debug/path-query"), r.URL.RawQuery)
}

func (s *Server) handleDebugHostPath(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "host=%s path=%s", r.Host, trimDebugPrefix(r.URL.Path, "/_gnar/debug/host-path"))
}

func (s *Server) handleDebugMethodPath(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "method=%s path=%s", r.Method, trimDebugPrefix(r.URL.Path, "/_gnar/debug/method-path"))
}

func (s *Server) handleDebugPrefixPath(w http.ResponseWriter, r *http.Request) {
	value := r.URL.Query().Get("value")
	fmt.Fprintf(w, "%s%s", value, trimDebugPrefix(r.URL.Path, "/_gnar/debug/prefix-path"))
}

func (s *Server) handleDebugStatic(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, r.URL.Query().Get("value"))
}

func trimDebugPrefix(value, prefix string) string {
	trimmed := strings.TrimPrefix(value, prefix)
	if trimmed == "" {
		return "/"
	}
	if !strings.HasPrefix(trimmed, "/") {
		return "/" + trimmed
	}
	return trimmed
}
