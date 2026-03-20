package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

// WorkflowHandler serves the workflow definition API.
//
//	PUT  /workflows/{name}  — store a YAML WorkflowDef
//	GET  /workflows/{name}  — retrieve a def as JSON
//	GET  /workflows         — list all defs as JSON
type WorkflowHandler struct {
	defs workflow.DefStore
	log  *slog.Logger
}

func NewWorkflowHandler(defs workflow.DefStore, log *slog.Logger) *WorkflowHandler {
	return &WorkflowHandler{defs: defs, log: log}
}

func (h *WorkflowHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Trim "/workflows" prefix; remainder is either "" or "/{name}".
	name := strings.TrimPrefix(r.URL.Path, "/workflows")
	name = strings.TrimPrefix(name, "/")

	switch {
	case r.Method == http.MethodGet && name == "":
		h.list(w, r)
	case r.Method == http.MethodGet && name != "":
		h.get(w, r, name)
	case r.Method == http.MethodPut && name != "":
		h.put(w, r, name)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (h *WorkflowHandler) list(w http.ResponseWriter, r *http.Request) {
	defs, err := h.defs.List(r.Context())
	if err != nil {
		h.log.Error("workflow list failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	sort.SliceStable(defs, func(i, j int) bool {
		if defs[i] == nil {
			return false
		}
		if defs[j] == nil {
			return true
		}
		return defs[i].Name < defs[j].Name
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(defs)
}

func (h *WorkflowHandler) get(w http.ResponseWriter, r *http.Request, name string) {
	def, err := h.defs.Get(r.Context(), name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(def)
}

func (h *WorkflowHandler) put(w http.ResponseWriter, r *http.Request, name string) {
	var def domain.WorkflowDef
	if err := yaml.NewDecoder(r.Body).Decode(&def); err != nil {
		http.Error(w, "invalid YAML: "+err.Error(), http.StatusBadRequest)
		return
	}
	if def.Name == "" {
		def.Name = name
	}
	if err := workflow.Validate(&def); err != nil {
		http.Error(w, "invalid workflow: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.defs.Put(r.Context(), name, &def); err != nil {
		h.log.Error("workflow put failed", "name", name, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(def)
}
