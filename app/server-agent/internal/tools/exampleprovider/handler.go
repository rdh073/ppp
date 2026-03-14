package exampleprovider

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode"
)

const ToolName = "identity.generate_alias_email"

var (
	inputSchema = json.RawMessage(`{
		"type":"object",
		"required":["fullName"],
		"properties":{
			"fullName":{"type":"string"},
			"domain":{"type":"string"},
			"suffix":{"type":"string"}
		}
	}`)
	outputSchema = json.RawMessage(`{
		"type":"object",
		"required":["email","localPart","domain","source"],
		"properties":{
			"email":{"type":"string"},
			"localPart":{"type":"string"},
			"domain":{"type":"string"},
			"source":{"type":"string"}
		}
	}`)
)

type invokeRequest struct {
	CallID string          `json:"callId"`
	Params json.RawMessage `json:"params"`
}

type invokeResponse struct {
	Result any          `json:"result,omitempty"`
	Error  *invokeError `json:"error,omitempty"`
}

type invokeError struct {
	Code      string `json:"code,omitempty"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}

type generateAliasEmailParams struct {
	FullName string `json:"fullName"`
	Domain   string `json:"domain,omitempty"`
	Suffix   string `json:"suffix,omitempty"`
}

type generateAliasEmailResult struct {
	Email     string `json:"email"`
	LocalPart string `json:"localPart"`
	Domain    string `json:"domain"`
	Source    string `json:"source"`
}

func NewHandler(log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/v1/tools", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, invokeResponse{Error: &invokeError{Code: "method_not_allowed", Message: "GET required"}})
			return
		}
		writeJSON(w, http.StatusOK, []map[string]any{toolDescriptor()})
	})
	mux.HandleFunc("/v1/tools/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, invokeResponse{Error: &invokeError{Code: "method_not_allowed", Message: "POST required"}})
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/v1/tools/")
		if !strings.HasSuffix(name, ":invoke") {
			writeJSON(w, http.StatusNotFound, invokeResponse{Error: &invokeError{Code: "not_found", Message: "unknown endpoint"}})
			return
		}
		toolName := strings.TrimSuffix(name, ":invoke")
		if toolName != ToolName {
			writeJSON(w, http.StatusNotFound, invokeResponse{Error: &invokeError{Code: "tool_not_found", Message: fmt.Sprintf("tool %s not found", toolName)}})
			return
		}

		var req invokeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, invokeResponse{Error: &invokeError{Code: "bad_request", Message: "decode request: " + err.Error()}})
			return
		}
		result, err := invokeAliasEmail(req.Params)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, invokeResponse{Error: &invokeError{Code: "invalid_params", Message: err.Error()}})
			return
		}
		if log != nil {
			log.Info("example tool invoked", "toolName", ToolName, "callId", req.CallID, "email", result.Email)
		}
		writeJSON(w, http.StatusOK, invokeResponse{Result: result})
	})
	return mux
}

func toolDescriptor() map[string]any {
	return map[string]any{
		"name":          ToolName,
		"description":   "Generates an alias email via the example HTTP provider",
		"deterministic": true,
		"timeout":       "2s",
		"retryBudget":   0,
		"inputSchema":   json.RawMessage(inputSchema),
		"outputSchema":  json.RawMessage(outputSchema),
	}
}

func invokeAliasEmail(raw json.RawMessage) (generateAliasEmailResult, error) {
	var params generateAliasEmailParams
	if len(raw) == 0 {
		return generateAliasEmailResult{}, fmt.Errorf("fullName is required")
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return generateAliasEmailResult{}, fmt.Errorf("decode params: %w", err)
	}
	params.FullName = strings.TrimSpace(params.FullName)
	params.Domain = strings.TrimSpace(strings.ToLower(params.Domain))
	params.Suffix = sanitizeToken(strings.TrimSpace(strings.ToLower(params.Suffix)))
	if params.FullName == "" {
		return generateAliasEmailResult{}, fmt.Errorf("fullName is required")
	}
	if params.Domain == "" {
		params.Domain = "remote.example.id"
	}
	localPart := localPartFromName(params.FullName)
	if params.Suffix != "" {
		localPart = localPart + "." + params.Suffix
	}
	return generateAliasEmailResult{
		Email:     localPart + "@" + params.Domain,
		LocalPart: localPart,
		Domain:    params.Domain,
		Source:    "example-http-provider",
	}, nil
}

func localPartFromName(fullName string) string {
	parts := strings.Fields(strings.ToLower(fullName))
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		token := sanitizeToken(part)
		if token != "" {
			cleaned = append(cleaned, token)
		}
	}
	if len(cleaned) == 0 {
		return "user"
	}
	return strings.Join(cleaned, ".")
}

func sanitizeToken(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case r == '.' || r == '-' || r == '_':
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), ".-_")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
