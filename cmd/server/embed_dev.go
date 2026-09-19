//go:build dev

package main

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.ReleaseMode)
}

func serveSPA(r *gin.Engine, _ string, _ bool) {
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws/") {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message": "Bedrock dev mode — frontend is served by Vite dev server",
		})
	})
}
