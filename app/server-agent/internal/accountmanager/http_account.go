package accountmanager

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// ---- AccountHandler ----

// AccountHandler serves:
//
//	GET  {pathPrefix}               — list accounts (filter: ?kind=google&deviceId=...)
//	POST {pathPrefix}/google        — register a Google account (called by Rhino script via http())
//	POST {pathPrefix}/instagram     — register an Instagram account (called by Rhino script via http())
type AccountHandler struct {
	service    *AccountService
	pathPrefix string
}

func NewAccountHandler(s *AccountService, pathPrefix string) *AccountHandler {
	return &AccountHandler{
		service:    s,
		pathPrefix: strings.TrimRight(pathPrefix, "/"),
	}
}

func (h *AccountHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, h.pathPrefix)
	path = strings.TrimPrefix(path, "/")

	switch path {
	case "", "/":
		if r.Method == http.MethodGet {
			h.list(w, r)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "google":
		if r.Method == http.MethodPost {
			h.register(w, r, "google")
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "instagram":
		if r.Method == http.MethodPost {
			h.register(w, r, "instagram")
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (h *AccountHandler) list(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	deviceID := r.URL.Query().Get("deviceId")
	items := h.service.List(kind, deviceID)
	if items == nil {
		jsonOK(w, []any{})
		return
	}
	jsonOK(w, items)
}

// register handles POST {pathPrefix}/{kind}.
// Body fields: email, username, password, personaId, linkedAccountId, deviceId.
// Called by Rhino scripts via the http() bridge function.
func (h *AccountHandler) register(w http.ResponseWriter, r *http.Request, kind string) {
	var body struct {
		DeviceID        string `json:"deviceId"`
		PersonaID       string `json:"personaId"`
		Email           string `json:"email"`
		Username        string `json:"username"`
		Password        string `json:"password"`
		LinkedAccountID string `json:"linkedAccountId"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	account, err := h.service.Register(kind, AccountRegistrationInput{
		DeviceID:        body.DeviceID,
		PersonaID:       body.PersonaID,
		Email:           body.Email,
		Username:        body.Username,
		Password:        body.Password,
		LinkedAccountID: body.LinkedAccountID,
	})
	if err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, map[string]string{"accountId": account.ID})
}

// ---- AccountToolHandler ----

// accountToolInvokeReq is the HTTP tool provider invoke request format.
type accountToolInvokeReq struct {
	CallID string          `json:"callId"`
	Params json.RawMessage `json:"params"`
}

type accountToolInvokeResp struct {
	Result any               `json:"result,omitempty"`
	Error  *accountToolError `json:"error,omitempty"`
}

type accountToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// AccountToolHandler serves the HTTP tool provider endpoints used by Rhino scripts:
//
//	POST /v1/tools/account.register_google:invoke
//	POST /v1/tools/account.register_instagram:invoke
type AccountToolHandler struct {
	service *AccountService
}

func NewAccountToolHandler(s *AccountService) *AccountToolHandler {
	return &AccountToolHandler{service: s}
}

func (h *AccountToolHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeToolError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/v1/tools/")
	name = strings.TrimSuffix(name, ":invoke")

	var kind string
	switch name {
	case "account.register_google":
		kind = "google"
	case "account.register_instagram":
		kind = "instagram"
	default:
		writeToolError(w, http.StatusNotFound, "tool_not_found", "unknown tool: "+name)
		return
	}

	var req accountToolInvokeReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeToolError(w, http.StatusBadRequest, "bad_request", "decode request: "+err.Error())
		return
	}

	var params struct {
		DeviceID        string `json:"deviceId"`
		PersonaID       string `json:"personaId"`
		Email           string `json:"email"`
		Username        string `json:"username"`
		Password        string `json:"password"`
		Contact         string `json:"contact"`
		GoogleAccountID string `json:"googleAccountId"`
		LinkedAccountID string `json:"linkedAccountId"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeToolError(w, http.StatusBadRequest, "bad_request", "decode params: "+err.Error())
		return
	}

	linkedID := params.LinkedAccountID
	if linkedID == "" {
		linkedID = params.GoogleAccountID
	}

	account, err := h.service.Register(kind, AccountRegistrationInput{
		DeviceID:        params.DeviceID,
		PersonaID:       params.PersonaID,
		Email:           params.Email,
		Username:        params.Username,
		Password:        params.Password,
		LinkedAccountID: linkedID,
	})
	if err != nil {
		writeToolError(w, http.StatusInternalServerError, "store_error", "save failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(accountToolInvokeResp{Result: map[string]string{"accountId": account.ID}})
}

func writeToolError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(accountToolInvokeResp{Error: &accountToolError{Code: code, Message: msg}})
}
