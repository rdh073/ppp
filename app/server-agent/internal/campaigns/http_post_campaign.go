package campaigns

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// PostCampaignHandler serves Instagram post campaigns.
//
//	POST {pathPrefix}       — start batch post campaign
//	GET  {pathPrefix}       — list all post campaigns
//	GET  {pathPrefix}/{id}  — get post campaign + per-job status
type PostCampaignHandler struct {
	service    *PostCampaignService
	pathPrefix string
}

func NewPostCampaignHandler(service *PostCampaignService, pathPrefix string) *PostCampaignHandler {
	return &PostCampaignHandler{
		service:    service,
		pathPrefix: strings.TrimRight(pathPrefix, "/"),
	}
}

func (h *PostCampaignHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

func (h *PostCampaignHandler) list(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, h.service.List())
}

func (h *PostCampaignHandler) get(w http.ResponseWriter, r *http.Request, id string) {
	campaign, ok := h.service.Get(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	jsonOK(w, campaign)
}

func (h *PostCampaignHandler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountIDs  []string `json:"accountIds"`
		ImageSource string   `json:"imageSource"`
		ImageBase64 string   `json:"imageBase64"`
		ImagePrompt string   `json:"imagePrompt"`
		TextSource  string   `json:"textSource"`
		TextContent string   `json:"textContent"`
		TextPrompt  string   `json:"textPrompt"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 20<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}

	campaign, err := h.service.Start(r.Context(), StartPostCampaignInput{
		AccountIDs:  body.AccountIDs,
		ImageSource: body.ImageSource,
		ImageBase64: body.ImageBase64,
		ImagePrompt: body.ImagePrompt,
		TextSource:  body.TextSource,
		TextContent: body.TextContent,
		TextPrompt:  body.TextPrompt,
	})
	if err != nil {
		status := http.StatusBadRequest
		msg := err.Error()
		switch {
		case strings.Contains(msg, "not configured"):
			status = http.StatusServiceUnavailable
		case strings.Contains(msg, "generate caption"), strings.Contains(msg, "generate image"), strings.Contains(msg, "create posts dir"), strings.Contains(msg, "save image"):
			status = http.StatusInternalServerError
		}
		http.Error(w, msg, status)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	jsonOK(w, map[string]string{"campaignId": campaign.ID})
}
