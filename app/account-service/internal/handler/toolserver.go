package handler

// ToolServerHandler exposes the account-service as an http tool provider.
//
//	GET  /v1/tools                                — discovery: list available tools
//	POST /v1/tools/account.register_google:invoke — register a Google account
//	POST /v1/tools/account.register_instagram:invoke — register an Instagram account
//
// This file is intentionally self-contained so it can be copied to a new domain
// service without pulling in any other handler files.

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/autosdk/ppp/account-service/internal/domain"
	"github.com/autosdk/ppp/account-service/internal/usecase"
)

// writeToolJSON is a local copy of the JSON helper, kept here so this file
// can stand alone when copied to a new service.
func writeToolJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type ToolServerHandler struct {
	uc  usecase.AccountRegistry
	log *slog.Logger
}

func NewToolServerHandler(uc usecase.AccountRegistry, log *slog.Logger) *ToolServerHandler {
	return &ToolServerHandler{uc: uc, log: log}
}

type invokeRequest struct {
	CallID string          `json:"callId"`
	Params json.RawMessage `json:"params"`
}

type invokeResponse struct {
	Result any          `json:"result,omitempty"`
	Error  *invokeError `json:"error,omitempty"`
}

type invokeError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}

func (h *ToolServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/v1/tools":
		h.listTools(w)
	case r.Method == http.MethodPost && strings.HasSuffix(path, ":invoke"):
		name := strings.TrimPrefix(path, "/v1/tools/")
		name = strings.TrimSuffix(name, ":invoke")
		h.invoke(w, r, name)
	default:
		writeToolJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (h *ToolServerHandler) listTools(w http.ResponseWriter) {
	writeToolJSON(w, http.StatusOK, []any{
		registerGoogleToolDescriptor(),
		registerInstagramToolDescriptor(),
	})
}

func (h *ToolServerHandler) invoke(w http.ResponseWriter, r *http.Request, name string) {
	var req invokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeToolJSON(w, http.StatusBadRequest, invokeResponse{
			Error: &invokeError{Code: "invalid_request", Message: "invalid JSON body"},
		})
		return
	}
	switch name {
	case "account.register_google":
		h.invokeRegisterGoogle(w, r, req)
	case "account.register_instagram":
		h.invokeRegisterInstagram(w, r, req)
	default:
		writeToolJSON(w, http.StatusNotFound, invokeResponse{
			Error: &invokeError{Code: "tool_not_found", Message: "unknown tool: " + name},
		})
	}
}

func (h *ToolServerHandler) invokeRegisterGoogle(w http.ResponseWriter, r *http.Request, req invokeRequest) {
	var params struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		DeviceID string `json:"deviceId"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeToolJSON(w, http.StatusBadRequest, invokeResponse{
			Error: &invokeError{Code: "invalid_params", Message: "invalid params: " + err.Error()},
		})
		return
	}
	a, err := h.uc.RegisterGoogle(r.Context(), usecase.RegisterGoogleRequest{
		Email:    params.Email,
		Password: params.Password,
		DeviceID: params.DeviceID,
	})
	if err != nil {
		h.log.Error("tool invoke RegisterGoogle failed", "err", err)
		writeToolJSON(w, http.StatusUnprocessableEntity, invokeResponse{
			Error: &invokeError{Code: "register_failed", Message: err.Error()},
		})
		return
	}
	writeToolJSON(w, http.StatusOK, invokeResponse{
		Result: map[string]string{"accountId": string(a.ID)},
	})
}

func (h *ToolServerHandler) invokeRegisterInstagram(w http.ResponseWriter, r *http.Request, req invokeRequest) {
	var params struct {
		Username        string `json:"username"`
		Password        string `json:"password"`
		Contact         string `json:"contact"`
		DeviceID        string `json:"deviceId"`
		GoogleAccountID string `json:"googleAccountId"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeToolJSON(w, http.StatusBadRequest, invokeResponse{
			Error: &invokeError{Code: "invalid_params", Message: "invalid params: " + err.Error()},
		})
		return
	}
	a, err := h.uc.RegisterInstagram(r.Context(), usecase.RegisterInstagramRequest{
		Username:        params.Username,
		Password:        params.Password,
		Contact:         params.Contact,
		DeviceID:        params.DeviceID,
		GoogleAccountID: domain.GoogleAccountID(params.GoogleAccountID),
	})
	if err != nil {
		h.log.Error("tool invoke RegisterInstagram failed", "err", err)
		writeToolJSON(w, http.StatusUnprocessableEntity, invokeResponse{
			Error: &invokeError{Code: "register_failed", Message: err.Error()},
		})
		return
	}
	writeToolJSON(w, http.StatusOK, invokeResponse{
		Result: map[string]string{"accountId": string(a.ID)},
	})
}

// --- tool descriptors ---

func registerGoogleToolDescriptor() map[string]any {
	return map[string]any{
		"name":        "account.register_google",
		"description": "Registers a newly created Google account in the account-service registry.",
		"inputSchema": json.RawMessage(`{
			"type":"object",
			"required":["email","password"],
			"properties":{
				"email":    {"type":"string"},
				"password": {"type":"string"},
				"deviceId": {"type":"string"}
			}
		}`),
		"outputSchema": json.RawMessage(`{
			"type":"object",
			"required":["accountId"],
			"properties":{
				"accountId":{"type":"string"}
			}
		}`),
	}
}

func registerInstagramToolDescriptor() map[string]any {
	return map[string]any{
		"name":        "account.register_instagram",
		"description": "Registers a newly created Instagram account in the account-service registry.",
		"inputSchema": json.RawMessage(`{
			"type":"object",
			"required":["username","password"],
			"properties":{
				"username":        {"type":"string"},
				"password":        {"type":"string"},
				"contact":         {"type":"string"},
				"deviceId":        {"type":"string"},
				"googleAccountId": {"type":"string"}
			}
		}`),
		"outputSchema": json.RawMessage(`{
			"type":"object",
			"required":["accountId"],
			"properties":{
				"accountId":{"type":"string"}
			}
		}`),
	}
}
