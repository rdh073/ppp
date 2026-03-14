package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/usecase"
)

// TaskHandler exposes task lifecycle over HTTP REST.
//
//	POST   /tasks          → create task
//	GET    /tasks/{id}     → get task
//	DELETE /tasks/{id}     → cancel task
type TaskHandler struct {
	uc  *usecase.TaskControlUseCase
	log *slog.Logger
}

func NewTaskHandler(uc *usecase.TaskControlUseCase, log *slog.Logger) *TaskHandler {
	return &TaskHandler{uc: uc, log: log}
}

func (h *TaskHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Trim "/tasks" prefix; remainder is either "" or "/{id}".
	id := strings.TrimPrefix(r.URL.Path, "/tasks")
	id = strings.TrimPrefix(id, "/")

	switch {
	case r.Method == http.MethodPost && id == "":
		h.create(w, r)
	case r.Method == http.MethodGet && id != "":
		h.get(w, r, domain.TaskID(id))
	case r.Method == http.MethodDelete && id != "":
		h.cancel(w, r, domain.TaskID(id))
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (h *TaskHandler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Goal           string            `json:"goal"`
		DeviceID       string            `json:"deviceId"`
		WorkflowName   string            `json:"workflowName"`
		InputArtifacts map[string]string `json:"inputArtifacts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Goal == "" {
		http.Error(w, "goal is required", http.StatusBadRequest)
		return
	}

	task, err := h.uc.CreateTask(r.Context(), usecase.CreateTaskRequest{
		Goal:           body.Goal,
		DeviceID:       domain.DeviceID(body.DeviceID),
		WorkflowName:   body.WorkflowName,
		InputArtifacts: body.InputArtifacts,
	})
	if err != nil {
		h.log.Error("CreateTask failed", "err", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(taskJSON(task))
}

func (h *TaskHandler) get(w http.ResponseWriter, r *http.Request, id domain.TaskID) {
	task, err := h.uc.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(taskJSON(task))
}

func (h *TaskHandler) cancel(w http.ResponseWriter, r *http.Request, id domain.TaskID) {
	if err := h.uc.CancelTask(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func taskJSON(t *domain.Task) map[string]any {
	return map[string]any{
		"id":             string(t.ID),
		"goal":           t.Goal,
		"inputArtifacts": t.InputArtifacts,
		"status":         string(t.Status),
		"assignedDevice": string(t.AssignedDevice),
		"createdAt":      t.CreatedAt,
		"updatedAt":      t.UpdatedAt,
	}
}
