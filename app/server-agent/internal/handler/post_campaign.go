package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/usecase"
)

// ---- PostCampaignHandler ----

// PostCampaignHandler manages Instagram batch post campaigns:
//
//	POST /posts/instagram           — start batch post
//	GET  /posts/instagram           — list all post campaigns
//	GET  /posts/instagram/{id}      — get post campaign + per-job status
type PostCampaignHandler struct {
	tasks    usecase.TaskControl
	accounts store.AccountStore
	captions usecase.CaptionGenerator // nil = caption generation disabled
	images   usecase.ImageGenerator   // nil = AI image generation disabled
	adb      usecase.AdbFilePusher    // nil = ADB push skipped
	dataDir  string                   // server data dir for storing temp images
	log      *slog.Logger

	mu        sync.RWMutex
	campaigns map[string]*domain.PostCampaign
}

func NewPostCampaignHandler(
	tasks usecase.TaskControl,
	accounts store.AccountStore,
	captions usecase.CaptionGenerator,
	images usecase.ImageGenerator,
	adb usecase.AdbFilePusher,
	dataDir string,
	log *slog.Logger,
) *PostCampaignHandler {
	return &PostCampaignHandler{
		tasks:     tasks,
		accounts:  accounts,
		captions:  captions,
		images:    images,
		adb:       adb,
		dataDir:   dataDir,
		log:       log,
		campaigns: make(map[string]*domain.PostCampaign),
	}
}

func (h *PostCampaignHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/posts/instagram")
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
	h.mu.RLock()
	out := make([]*domain.PostCampaign, 0, len(h.campaigns))
	for _, c := range h.campaigns {
		cp := *c
		out = append(out, &cp)
	}
	h.mu.RUnlock()
	jsonOK(w, out)
}

func (h *PostCampaignHandler) get(w http.ResponseWriter, r *http.Request, id string) {
	h.mu.RLock()
	c, ok := h.campaigns[id]
	h.mu.RUnlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	h.mu.RLock()
	cp := *c
	h.mu.RUnlock()
	jsonOK(w, cp)
}

func (h *PostCampaignHandler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountIDs  []string `json:"accountIds"`
		ImageSource string   `json:"imageSource"` // "manual"|"ai"
		ImageBase64 string   `json:"imageBase64"` // if imageSource=manual
		ImagePrompt string   `json:"imagePrompt"` // if imageSource=ai
		TextSource  string   `json:"textSource"`  // "manual"|"ai"
		TextContent string   `json:"textContent"` // if textSource=manual
		TextPrompt  string   `json:"textPrompt"`  // if textSource=ai
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 20<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(body.AccountIDs) == 0 {
		http.Error(w, "accountIds is required (at least one)", http.StatusBadRequest)
		return
	}
	if body.ImageSource != "manual" && body.ImageSource != "ai" {
		http.Error(w, "imageSource must be 'manual' or 'ai'", http.StatusBadRequest)
		return
	}
	if body.TextSource != "manual" && body.TextSource != "ai" {
		http.Error(w, "textSource must be 'manual' or 'ai'", http.StatusBadRequest)
		return
	}

	// ── Resolve accounts (must be active + have deviceId) ────────────────────
	type resolvedAccount struct {
		account  domain.Account
		deviceID string
	}
	var resolved []resolvedAccount
	for _, id := range body.AccountIDs {
		a, ok := h.accounts.GetByID(id)
		if !ok {
			http.Error(w, "account not found: "+id, http.StatusBadRequest)
			return
		}
		if a.Status != domain.AccountStatusActive {
			http.Error(w, fmt.Sprintf("account %s is not active (status: %s)", id, a.Status), http.StatusBadRequest)
			return
		}
		if a.DeviceID == "" {
			http.Error(w, fmt.Sprintf("account %s has no device bound", id), http.StatusBadRequest)
			return
		}
		resolved = append(resolved, resolvedAccount{account: a, deviceID: a.DeviceID})
	}

	// ── Resolve caption ───────────────────────────────────────────────────────
	caption := body.TextContent
	if body.TextSource == "ai" {
		if body.TextPrompt == "" {
			http.Error(w, "textPrompt is required when textSource=ai", http.StatusBadRequest)
			return
		}
		if h.captions == nil {
			http.Error(w, "AI caption generation is not configured (no LLM API key)", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		var err error
		caption, err = h.captions.GenerateCaption(ctx, body.TextPrompt)
		if err != nil {
			http.Error(w, "generate caption: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if caption == "" {
		http.Error(w, "caption is empty — provide textContent or textPrompt", http.StatusBadRequest)
		return
	}

	// ── Resolve image bytes ───────────────────────────────────────────────────
	var imgBytes []byte
	switch body.ImageSource {
	case "manual":
		if body.ImageBase64 == "" {
			http.Error(w, "imageBase64 is required when imageSource=manual", http.StatusBadRequest)
			return
		}
		var err error
		imgBytes, err = base64.StdEncoding.DecodeString(body.ImageBase64)
		if err != nil {
			// try raw URL-safe base64
			imgBytes, err = base64.URLEncoding.DecodeString(body.ImageBase64)
			if err != nil {
				http.Error(w, "imageBase64: invalid base64: "+err.Error(), http.StatusBadRequest)
				return
			}
		}
	case "ai":
		if body.ImagePrompt == "" {
			http.Error(w, "imagePrompt is required when imageSource=ai", http.StatusBadRequest)
			return
		}
		if h.images == nil {
			http.Error(w, "AI image generation is not configured (set AUTO_TOOL_OPENAI_API_KEY)", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		var err error
		imgBytes, err = h.images.GenerateImage(ctx, body.ImagePrompt)
		if err != nil {
			http.Error(w, "generate image: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// ── Persist image to server disk ──────────────────────────────────────────
	campaignID := domain.NewPostCampaignID()
	postsDir := filepath.Join(h.dataDir, "posts")
	if err := os.MkdirAll(postsDir, 0o755); err != nil {
		http.Error(w, "create posts dir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	localImagePath := filepath.Join(postsDir, campaignID+".jpg")
	if err := os.WriteFile(localImagePath, imgBytes, 0o644); err != nil {
		http.Error(w, "save image: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// ── Build campaign ────────────────────────────────────────────────────────
	now := time.Now()
	jobs := make([]domain.PostJob, len(resolved))
	for i, ra := range resolved {
		jobs[i] = domain.PostJob{
			ID:        domain.NewPostJobID(),
			AccountID: ra.account.ID,
			DeviceID:  ra.deviceID,
			Status:    "pending",
		}
	}

	c := &domain.PostCampaign{
		ID:          campaignID,
		ImageSource: body.ImageSource,
		TextSource:  body.TextSource,
		Caption:     caption,
		Jobs:        jobs,
		Status:      "running",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	h.mu.Lock()
	h.campaigns[c.ID] = c
	h.mu.Unlock()

	go h.run(context.Background(), c, localImagePath)

	w.WriteHeader(http.StatusAccepted)
	jsonOK(w, map[string]string{"postCampaignId": c.ID})
}

// run executes all post jobs in parallel.
func (h *PostCampaignHandler) run(ctx context.Context, c *domain.PostCampaign, localImagePath string) {
	var wg sync.WaitGroup
	for i := range c.Jobs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			h.runJob(ctx, c, idx, localImagePath)
		}(i)
	}
	wg.Wait()

	// Determine overall campaign status.
	h.mu.Lock()
	allDone := true
	for _, j := range c.Jobs {
		if j.Status != "done" && j.Status != "failed" {
			allDone = false
			break
		}
	}
	if allDone {
		c.Status = "done"
	}
	c.UpdatedAt = time.Now()
	h.mu.Unlock()

	h.log.Info("post campaign finished", "campaignId", c.ID)
}

func (h *PostCampaignHandler) runJob(ctx context.Context, c *domain.PostCampaign, idx int, localImagePath string) {
	job := c.Jobs[idx]
	h.jUpdate(c, idx, func(j *domain.PostJob) {
		j.Status = "running"
		j.Error = ""
	})

	req := usecase.PostJobRequest{
		CampaignID:     c.ID,
		JobID:          job.ID,
		AccountID:      job.AccountID,
		DeviceID:       domain.DeviceID(job.DeviceID),
		LocalImagePath: localImagePath,
		RemoteImageDir: "/sdcard/DCIM/ppp_posts",
		Caption:        c.Caption,
		WorkflowName:   "instagram-post-script",
	}
	deps := usecase.PostJobDeps{Tasks: h.tasks, Adb: h.adb}

	taskID, err := usecase.ExecutePostJob(ctx, req, deps, h.log)
	if taskID != "" {
		h.jUpdate(c, idx, func(j *domain.PostJob) { j.TaskID = string(taskID) })
	}
	if err != nil {
		h.failJob(c, idx, err.Error())
		return
	}

	h.jUpdate(c, idx, func(j *domain.PostJob) {
		j.Status = "done"
		j.Error = ""
	})
	h.log.Info("post job done", "campaignId", c.ID, "jobId", job.ID, "accountId", job.AccountID)
}

func (h *PostCampaignHandler) jUpdate(c *domain.PostCampaign, idx int, fn func(*domain.PostJob)) {
	h.mu.Lock()
	fn(&c.Jobs[idx])
	c.UpdatedAt = time.Now()
	h.mu.Unlock()
}

func (h *PostCampaignHandler) failJob(c *domain.PostCampaign, idx int, reason string) {
	h.jUpdate(c, idx, func(j *domain.PostJob) {
		j.Status = "failed"
		j.Error = reason
	})
	h.log.Warn("post job failed", "campaignId", c.ID, "jobId", c.Jobs[idx].ID, "reason", reason)
}
