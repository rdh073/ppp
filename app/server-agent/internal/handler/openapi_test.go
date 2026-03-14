package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/handler"
)

func TestOpenAPISpecHandler_ServesJSON(t *testing.T) {
	h := handler.NewOpenAPISpecHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("unexpected content type: %q", contentType)
	}

	var payload struct {
		OpenAPI string         `json:"openapi"`
		Paths   map[string]any `json:"paths"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode openapi.json: %v", err)
	}
	if payload.OpenAPI != "3.1.0" {
		t.Fatalf("unexpected openapi version: %q", payload.OpenAPI)
	}
	for _, path := range []string{"/tasks", "/workflows", "/events/accepted", "/metrics"} {
		if _, ok := payload.Paths[path]; ok {
			continue
		}
		t.Fatalf("expected path %q in spec", path)
	}
}

func TestSwaggerUIHandler_ServesHTML(t *testing.T) {
	h := handler.NewSwaggerUIHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/swagger/", nil)

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("unexpected content type: %q", contentType)
	}
	body := rec.Body.String()
	for _, needle := range []string{"SwaggerUIBundle", "/openapi.json", "PPP server-agent Swagger"} {
		if strings.Contains(body, needle) {
			continue
		}
		t.Fatalf("expected %q in swagger page, got body:\n%s", needle, body)
	}
}

func TestSwaggerUIHandler_SubpathReturns404(t *testing.T) {
	h := handler.NewSwaggerUIHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/swagger/assets.js", nil)

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
