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
	fileServer := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if f, err := dist.Open(p); err == nil {
			st, statErr := f.Stat()
			_ = f.Close()
			if statErr == nil && !st.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// path missing or a directory: fall back to index.html for the frontend router
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
