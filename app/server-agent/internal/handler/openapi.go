package handler

import (
	_ "embed"
	"net/http"
)

//go:embed assets/openapi.json
var openAPISpec []byte

//go:embed assets/swagger.html
var swaggerUIPage []byte

// NewOpenAPISpecHandler serves the static OpenAPI document for the HTTP control plane.
func NewOpenAPISpecHandler() http.Handler {
	return newStaticDocHandler("application/json; charset=utf-8", openAPISpec, "/openapi.json")
}

// NewSwaggerUIHandler serves a lightweight Swagger UI shell that points at /openapi.json.
func NewSwaggerUIHandler() http.Handler {
	return newStaticDocHandler("text/html; charset=utf-8", swaggerUIPage, "/swagger", "/swagger/")
}

func newStaticDocHandler(contentType string, body []byte, paths ...string) http.Handler {
	allowed := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		allowed[path] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allowed[r.URL.Path]; !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	})
}
