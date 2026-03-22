package accountmanager

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// LoginRouteConfig holds platform-specific parameters for login runs.
type LoginRouteConfig struct {
	PathPrefix string
	ServiceCfg LoginRunConfig
}

func GoogleLoginConfig() LoginRouteConfig {
	return LoginRouteConfig{
		PathPrefix: "/account-manager/logins/google",
		ServiceCfg: LoginRunConfig{
			Platform:        "google",
			AccountKind:     "google",
			PackagesToClear: []string{"com.google.android.gms", "com.google.android.googlequicksearchbox"},
			WorkflowName:    "google-account-login-script",
		},
	}
}

func InstagramLoginConfig() LoginRouteConfig {
	return LoginRouteConfig{
		PathPrefix: "/account-manager/logins/instagram",
		ServiceCfg: LoginRunConfig{
			Platform:        "instagram",
			AccountKind:     "instagram",
			PackagesToClear: []string{"com.instagram.android"},
			WorkflowName:    "instagram-account-login-script",
		},
	}
}

// LoginRunHandler serves platform account login runs.
//
//	POST {pathPrefix}       — start a login run
//	GET  {pathPrefix}       — list login runs
//	GET  {pathPrefix}/{id}  — get login-run status
type LoginRunHandler struct {
	service    *LoginRunService
	pathPrefix string
}

func NewLoginRunHandler(service *LoginRunService, pathPrefix string) *LoginRunHandler {
	return &LoginRunHandler{
		service:    service,
		pathPrefix: strings.TrimRight(pathPrefix, "/"),
	}
}

func (h *LoginRunHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

func (h *LoginRunHandler) list(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, h.service.List())
}

func (h *LoginRunHandler) get(w http.ResponseWriter, r *http.Request, id string) {
	run, ok := h.service.Get(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, run)
}

func (h *LoginRunHandler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID string `json:"accountId"`
		Email     string `json:"email"`
		Password  string `json:"password"`
		DeviceID  string `json:"deviceId"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}

	run, account, err := h.service.Start(r.Context(), StartLoginRunInput{
		AccountID: body.AccountID,
		Email:     body.Email,
		Password:  body.Password,
		DeviceID:  body.DeviceID,
	})
	if err != nil {
		status := http.StatusBadRequest
		if strings.HasPrefix(err.Error(), "account not found: ") {
			status = http.StatusNotFound
		} else if strings.HasPrefix(err.Error(), "save account: ") {
			status = http.StatusInternalServerError
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	jsonOK(w, map[string]string{"loginRunId": run.ID, "accountId": account.ID})
}
