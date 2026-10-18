package server

import (
	"io/fs"
	"net/http"
	"strings"
)

// SPAHandler returns a static file handler with history-mode fallback: paths matching a
// real file are served directly; everything else falls back to index.html for the frontend
// router. API routes must be registered before the SPA handler to avoid being swallowed.
func SPAHandler(dist fs.FS) http.Handler {
	if dist == nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if f, err := dist.Open(p); err == nil {
			st, statErr := f.Stat()
			_ = f.Close()
			if statErr == nil && !st.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		if strings.Contains(p, ".") || strings.HasPrefix(p, "assets/") {
			http.NotFound(w, r)
			return
		}
		clone := r.Clone(r.Context())
		clone.URL.Path = "/"
		w.Header().Set("Cache-Control", "no-store")
		fileServer.ServeHTTP(w, clone)
	})
}
