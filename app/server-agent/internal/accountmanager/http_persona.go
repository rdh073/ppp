package accountmanager

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// ---- PersonaHandler ----

// PersonaHandler serves GET/POST/DELETE {pathPrefix} and GET/DELETE {pathPrefix}/{id}.
type PersonaHandler struct {
	service    *PersonaService
	pathPrefix string
}

func NewPersonaHandler(s *PersonaService, pathPrefix string) *PersonaHandler {
	return &PersonaHandler{
		service:    s,
		pathPrefix: strings.TrimRight(pathPrefix, "/"),
	}
}

func (h *PersonaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, h.pathPrefix)
	path = strings.TrimPrefix(path, "/")

	switch path {
	case "", "/":
		switch r.Method {
		case http.MethodGet:
			h.list(w, r)
		case http.MethodPost:
			h.create(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		switch r.Method {
		case http.MethodGet:
			h.getOne(w, r, path)
		case http.MethodDelete:
			h.deleteOne(w, r, path)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func (h *PersonaHandler) list(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	status := r.URL.Query().Get("status")
	items := h.service.List(kind, status)
	if items == nil {
		jsonOK(w, []any{})
		return
	}
	jsonOK(w, items)
}

func (h *PersonaHandler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind      string `json:"kind"`
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
		Gender    string `json:"gender"`
		BirthDate string `json:"birthDate"`
		Email     string `json:"email"`
		Username  string `json:"username"`
		Password  string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	persona, err := h.service.Create(CreatePersonaInput{
		Kind:      body.Kind,
		FirstName: body.FirstName,
		LastName:  body.LastName,
		Gender:    body.Gender,
		BirthDate: body.BirthDate,
		Email:     body.Email,
		Username:  body.Username,
		Password:  body.Password,
	})
	if err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, persona)
}

func (h *PersonaHandler) getOne(w http.ResponseWriter, r *http.Request, id string) {
	p, ok := h.service.Get(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, p)
}

func (h *PersonaHandler) deleteOne(w http.ResponseWriter, r *http.Request, id string) {
	found, err := h.service.Delete(id)
	if err != nil {
		http.Error(w, "delete failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
