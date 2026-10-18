// Package server assembles HTTP routes and handlers.
package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ravenmk2/octodcp/web"
)

// New assembles routes and returns the root HTTP handler.
func New() http.Handler {
	r := gin.Default()
	r.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// SPA fallback: unmatched routes fall back to index.html for the frontend router
	r.NoRoute(gin.WrapH(SPAHandler(web.DistFS())))
	return r
}
