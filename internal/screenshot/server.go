package screenshot

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Handler serves the built web UI and the fixed demo API used for screenshots.
func Handler(web fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", serveScreenshotAPI)
	mux.HandleFunc("/v1/", serveScreenshotAPI)
	mux.Handle("/", spa(web))
	return mux
}

func spa(web fs.FS) http.Handler {
	files := http.FileServer(http.FS(web))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The page names its screen once, before it rewrites its own address.
		if r.URL.Query().Has("screen") {
			currentScreen.Store(r.URL.Query().Get("screen"))
		}
		if r.URL.Path != "/" {
			cleaned := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
			if cleaned != "" && cleaned != "." {
				if _, err := fs.Stat(web, cleaned); err != nil {
					r = r.Clone(r.Context())
					r.URL.Path = "/"
				}
			}
		}
		files.ServeHTTP(w, r)
	})
}
