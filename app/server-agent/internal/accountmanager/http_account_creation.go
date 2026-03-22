package accountmanager

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// AccountCreationHandler serves account-creation runs.
//
//	POST {pathPrefix}       — start an account-creation run
//	GET  {pathPrefix}       — list account-creation runs
//	GET  {pathPrefix}/{id}  — get account-creation status
type AccountCreationHandler struct {
	service    *AccountCreationService
	pathPrefix string
}

func NewAccountCreationHandler(service *AccountCreationService, pathPrefix string) *AccountCreationHandler {
	return &AccountCreationHandler{
		service:    service,
		pathPrefix: strings.TrimRight(pathPrefix, "/"),
	}
}

func (h *AccountCreationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, h.pathPrefix)
	path = strings.TrimPrefix(path, "/")

	switch path {
	case "", "/":
		switch r.Method {
		case http.MethodPost:
			h.create(w, r)
		case http.MethodGet:
			h.list(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		if r.Method == http.MethodGet {
			h.get(w, r, path)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func (h *AccountCreationHandler) list(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, h.service.List())
}

func (h *AccountCreationHandler) get(w http.ResponseWriter, r *http.Request, id string) {
	run, ok := h.service.Get(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, run)
}

func (h *AccountCreationHandler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind            string `json:"kind"`
		DeviceID        string `json:"deviceId"`
		PersonaID       string `json:"personaId"`
		CaptchaEndpoint string `json:"captchaEndpoint"`
		PhoneNumber     string `json:"phoneNumber"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}

	run, err := h.service.Start(r.Context(), StartAccountCreationInput{
		Kind:            body.Kind,
		DeviceID:        body.DeviceID,
		PersonaID:       body.PersonaID,
		PhoneNumber:     body.PhoneNumber,
		CaptchaEndpoint: body.CaptchaEndpoint,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	jsonOK(w, map[string]string{"accountCreationId": run.ID})
}
