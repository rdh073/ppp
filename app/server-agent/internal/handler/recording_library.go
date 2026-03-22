package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/autosdk/ppp/server-agent/internal/store"
)

// ---- MacroLibraryHandler ----

// WorkflowReloader is an optional dependency that reloads the workflow def store
// immediately after a new workflow file is written (e.g. *workflow.FSDefStore).
type WorkflowReloader interface {
	ReloadNow(ctx context.Context) error
}

// MacroLibraryHandler serves GET/DELETE /macros and POST /macros/{id}/promote.
type MacroLibraryHandler struct {
	library     store.MacroStore
	workflowDir string
	reloader    WorkflowReloader
}

func NewMacroLibraryHandler(library store.MacroStore, workflowDir string) *MacroLibraryHandler {
	return &MacroLibraryHandler{library: library, workflowDir: workflowDir}
}

// WithReloader attaches an optional workflow reloader (called after promote writes a file).
func (h *MacroLibraryHandler) WithReloader(r WorkflowReloader) *MacroLibraryHandler {
	h.reloader = r
	return h
}

func (h *MacroLibraryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/macros")
	path = strings.TrimPrefix(path, "/")

	// /macros/{id}/promote
	if strings.HasSuffix(path, "/promote") {
		id := strings.TrimSuffix(path, "/promote")
		h.handlePromote(w, r, id)
		return
	}

	switch path {
	case "", "/":
		h.handleList(w, r)
	default:
		h.handleOne(w, r, path)
	}
}

// GET /macros
func (h *MacroLibraryHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	items := h.library.List()
	if items == nil {
		items = []store.SavedMacro{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(items)
}

// GET /macros/{id}   DELETE /macros/{id}
func (h *MacroLibraryHandler) handleOne(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	switch r.Method {
	case http.MethodGet:
		macro, ok := h.library.GetByID(id)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(macro)

	case http.MethodDelete:
		found, err := h.library.Delete(id)
		if err != nil {
			http.Error(w, "delete failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if !found {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// POST /macros/{id}/promote — generates a workflow YAML and writes it to workflowDir.
func (h *MacroLibraryHandler) handlePromote(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.workflowDir == "" {
		http.Error(w, "workflow-dir not configured on this server", http.StatusNotImplemented)
		return
	}

	macro, ok := h.library.GetByID(id)
	if !ok {
		http.Error(w, "macro not found", http.StatusNotFound)
		return
	}

	var body struct {
		WorkflowName string `json:"workflowName"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)

	name := body.WorkflowName
	if name == "" {
		name = macro.WorkflowName
	}
	if name == "" {
		name = "macro-" + macro.ID
	}

	yaml := generateWorkflowYAML(name, macro.Script)
	safeName := sanitizeFilename(name)
	filePath := filepath.Join(h.workflowDir, safeName+".yaml")

	if err := os.WriteFile(filePath, []byte(yaml), 0o644); err != nil {
		http.Error(w, fmt.Sprintf("write workflow file: %v", err), http.StatusInternalServerError)
		return
	}

	if h.reloader != nil {
		if err := h.reloader.ReloadNow(r.Context()); err != nil {
			// Non-fatal: file was written, polling will pick it up.
			_ = err
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"workflowName": name,
		"path":         filePath,
	})
}

var nonAlphanumDash = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func sanitizeFilename(name string) string {
	s := strings.ReplaceAll(name, " ", "-")
	s = nonAlphanumDash.ReplaceAllString(s, "")
	if s == "" {
		return "macro"
	}
	return s
}
