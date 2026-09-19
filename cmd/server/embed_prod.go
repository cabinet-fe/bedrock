//go:build !dev

package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed all:dist
var webFS embed.FS

func init() {
	gin.SetMode(gin.ReleaseMode)
}

// injectBootstrapConfig splices the runtime bootstrap (login encryption key,
// register toggle) into the SPA shell before </head>.
func injectBootstrapConfig(html []byte, keyHex string, allowRegister bool) []byte {
	keyJSON, err := json.Marshal(keyHex)
	if err != nil {
		keyJSON = []byte(`""`)
	}
	snippet := `<script>window.__BEDROCK_ENCRYPTION_KEY__=` + string(keyJSON) +
		`;window.__BEDROCK_ALLOW_REGISTER__=` + strconv.FormatBool(allowRegister) + `</script>`
	const marker = "</head>"
	idx := bytes.Index(html, []byte(marker))
	if idx < 0 {
		return html
	}
	out := make([]byte, 0, len(html)+len(snippet))
	out = append(out, html[:idx]...)
	out = append(out, snippet...)
	out = append(out, html[idx:]...)
	return out
}

const (
	noStoreCacheControl  = "no-cache, no-store, must-revalidate"
	shortTTLCacheControl = "public, max-age=3600"
	longTTLCacheControl  = "public, max-age=31536000, immutable"
)

func hasBuildFingerprint(filePath string) bool {
	fileName := path.Base(filePath)
	extIdx := strings.LastIndex(fileName, ".")
	if extIdx <= 0 {
		return false
	}
	nameWithoutExt := fileName[:extIdx]
	sepIdx := strings.LastIndex(nameWithoutExt, "-")
	if sepIdx < 0 || sepIdx == len(nameWithoutExt)-1 {
		return false
	}
	fingerprint := nameWithoutExt[sepIdx+1:]
	if len(fingerprint) < 8 {
		return false
	}
	for _, ch := range fingerprint {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') {
			return false
		}
	}
	return true
}

func cacheControlForStaticFile(filePath string) string {
	if hasBuildFingerprint(filePath) {
		return longTTLCacheControl
	}
	return shortTTLCacheControl
}

func serveSPA(r *gin.Engine, encryptionKeyHex string, allowRegister bool) {
	distFS, err := fs.Sub(webFS, "dist")
	if err != nil {
		return
	}
	indexHTML, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		return
	}
	staticServer := http.FileServer(http.FS(distFS))

	serveIndex := func(c *gin.Context) {
		html := injectBootstrapConfig(indexHTML, encryptionKeyHex, allowRegister)
		c.Header("Cache-Control", noStoreCacheControl)
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.Data(http.StatusOK, "text/html; charset=utf-8", html)
	}

	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws/") {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "not found"})
			return
		}

		trimmedPath := strings.TrimPrefix(path, "/")
		if trimmedPath == "" || trimmedPath == "index.html" {
			serveIndex(c)
			return
		}

		if fileInfo, err := fs.Stat(distFS, trimmedPath); err == nil && !fileInfo.IsDir() {
			c.Header("Cache-Control", cacheControlForStaticFile(trimmedPath))
			c.Request.URL.Path = path
			staticServer.ServeHTTP(c.Writer, c.Request)
			return
		}

		serveIndex(c)
	})
}
