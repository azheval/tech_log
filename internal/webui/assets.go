// Package webui embeds the standalone browser client for the tech-log API.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html app.js styles.css
var assets embed.FS

// FS returns the embedded UI filesystem for callers needing a custom mount.
func FS() fs.FS { return assets }

// Handler serves the UI with a strict, external-assets-only CSP. API routes
// remain the responsibility of the caller.
func Handler() http.Handler {
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		files.ServeHTTP(w, r)
	})
}
