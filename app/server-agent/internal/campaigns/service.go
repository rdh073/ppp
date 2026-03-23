package campaigns

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

type StartPostCampaignInput struct {
	AccountIDs  []string
	ImageSource string
	ImageBase64 string
	ImagePrompt string
	TextSource  string
	TextContent string
	TextPrompt  string
}

type PostCampaignAICapability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type PostCampaignCapabilities struct {
	ImageAI PostCampaignAICapability `json:"imageAI"`
	TextAI  PostCampaignAICapability `json:"textAI"`
}

type PostCampaignService struct {
	tasks    TaskControl
	accounts store.AccountStore
	captions CaptionGenerator
	images   ImageGenerator
	adb      AdbFilePusher
	publish  ProjectionPublisher
	dataDir  string
	log      *slog.Logger

	mu        sync.RWMutex
	campaigns map[string]*domain.PostCampaign
}

func NewPostCampaignService(
	tasks TaskControl,
	accounts store.AccountStore,
	captions CaptionGenerator,
	images ImageGenerator,
	adb AdbFilePusher,
	publish ProjectionPublisher,
	dataDir string,
	log *slog.Logger,
) *PostCampaignService {
	return &PostCampaignService{
		tasks:     tasks,
		accounts:  accounts,
		captions:  captions,
		images:    images,
		adb:       adb,
		publish:   publish,
		dataDir:   dataDir,
		log:       log,
		campaigns: make(map[string]*domain.PostCampaign),
	}
}

func (s *PostCampaignService) List() []*domain.PostCampaign {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*domain.PostCampaign, 0, len(s.campaigns))
	for _, campaign := range s.campaigns {
		cp := clonePostCampaign(campaign)
		out = append(out, &cp)
	}
	return out
}

func (s *PostCampaignService) Get(id string) (*domain.PostCampaign, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	campaign, ok := s.campaigns[id]
	if !ok {
		return nil, false
	}
	cp := clonePostCampaign(campaign)
	return &cp, true
}

func (s *PostCampaignService) Capabilities() PostCampaignCapabilities {
	caps := PostCampaignCapabilities{
		ImageAI: PostCampaignAICapability{Available: s.images != nil},
		TextAI:  PostCampaignAICapability{Available: s.captions != nil},
	}
	if !caps.ImageAI.Available {
		caps.ImageAI.Reason = "AI image generation is not configured on the server."
	}
	if !caps.TextAI.Available {
		caps.TextAI.Reason = "AI caption generation is not configured on the server."
	}
	return caps
}

func (s *PostCampaignService) Start(ctx context.Context, input StartPostCampaignInput) (*domain.PostCampaign, error) {
	if len(input.AccountIDs) == 0 {
		return nil, errors.New("accountIds is required (at least one)")
	}
	if input.ImageSource != "manual" && input.ImageSource != "ai" {
		return nil, errors.New("imageSource must be 'manual' or 'ai'")
	}
	if input.TextSource != "manual" && input.TextSource != "ai" {
		return nil, errors.New("textSource must be 'manual' or 'ai'")
	}

	type resolvedAccount struct {
		account  domain.Account
		deviceID string
	}
	var resolved []resolvedAccount
	for _, id := range input.AccountIDs {
		account, ok := s.accounts.GetByID(id)
		if !ok {
			return nil, fmt.Errorf("account not found: %s", id)
		}
		if account.Status != domain.AccountStatusActive {
			return nil, fmt.Errorf("account %s is not active (status: %s)", id, account.Status)
		}
		if account.DeviceID == "" {
			return nil, fmt.Errorf("account %s has no device bound", id)
		}
		resolved = append(resolved, resolvedAccount{account: account, deviceID: account.DeviceID})
	}

	caption := input.TextContent
	if input.TextSource == "ai" {
		if input.TextPrompt == "" {
			return nil, errors.New("textPrompt is required when textSource=ai")
		}
		if s.captions == nil {
			return nil, errors.New("AI caption generation is not configured (no LLM API key)")
		}
		captionCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		generated, err := s.captions.GenerateCaption(captionCtx, input.TextPrompt)
		if err != nil {
			return nil, fmt.Errorf("generate caption: %w", err)
		}
		caption = generated
	}
	if caption == "" {
		return nil, errors.New("caption is empty — provide textContent or textPrompt")
	}

	var imgBytes []byte
	switch input.ImageSource {
	case "manual":
		if input.ImageBase64 == "" {
			return nil, errors.New("imageBase64 is required when imageSource=manual")
		}
		decoded, err := base64.StdEncoding.DecodeString(input.ImageBase64)
		if err != nil {
			decoded, err = base64.URLEncoding.DecodeString(input.ImageBase64)
			if err != nil {
				return nil, fmt.Errorf("imageBase64: invalid base64: %w", err)
			}
		}
		imgBytes = decoded
	case "ai":
		if input.ImagePrompt == "" {
			return nil, errors.New("imagePrompt is required when imageSource=ai")
		}
		if s.images == nil {
			return nil, errors.New("AI image generation is not configured (set AUTO_TOOL_OPENAI_API_KEY)")
		}
		imageCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		generated, err := s.images.GenerateImage(imageCtx, input.ImagePrompt)
		if err != nil {
			return nil, fmt.Errorf("generate image: %w", err)
		}
		imgBytes = generated
	}

	campaignID := domain.NewPostCampaignID()
	postsDir := filepath.Join(s.dataDir, "posts")
	if err := os.MkdirAll(postsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create posts dir: %w", err)
	}
	localImagePath := filepath.Join(postsDir, campaignID+".jpg")
	if err := os.WriteFile(localImagePath, imgBytes, 0o644); err != nil {
		return nil, fmt.Errorf("save image: %w", err)
	}

	now := time.Now()
	jobs := make([]domain.PostJob, len(resolved))
	for i, resolvedAccount := range resolved {
		jobs[i] = domain.PostJob{
			ID:        domain.NewPostJobID(),
			AccountID: resolvedAccount.account.ID,
			DeviceID:  resolvedAccount.deviceID,
			Status:    "pending",
		}
	}

	campaign := &domain.PostCampaign{
		ID:          campaignID,
		ImageSource: input.ImageSource,
		TextSource:  input.TextSource,
		Caption:     caption,
		Jobs:        jobs,
		Status:      "running",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.mu.Lock()
	s.campaigns[campaign.ID] = campaign
	s.mu.Unlock()
	s.publishCampaign(campaign, "upsert")

	go s.run(context.Background(), campaign, localImagePath)

	cp := clonePostCampaign(campaign)
	return &cp, nil
}

func (s *PostCampaignService) GenerateImagePreview(ctx context.Context, prompt string) ([]byte, error) {
	if prompt == "" {
		return nil, errors.New("prompt is required")
	}
	if s.images == nil {
		return nil, errors.New("AI image generation is not configured")
	}
	return s.images.GenerateImage(ctx, prompt)
}

func (s *PostCampaignService) GenerateCaptionPreview(ctx context.Context, prompt string) (string, error) {
	if prompt == "" {
		return "", errors.New("prompt is required")
	}
	if s.captions == nil {
		return "", errors.New("AI caption generation is not configured")
	}
	return s.captions.GenerateCaption(ctx, prompt)
}

func (s *PostCampaignService) GenerateCaptionPreviewStream(ctx context.Context, prompt string, onChunk func(string)) error {
	if prompt == "" {
		return errors.New("prompt is required")
	}
	if s.captions == nil {
		return errors.New("AI caption generation is not configured")
	}
	return s.captions.GenerateCaptionStream(ctx, prompt, onChunk)
}

func (s *PostCampaignService) run(ctx context.Context, campaign *domain.PostCampaign, localImagePath string) {
	var wg sync.WaitGroup
	for i := range campaign.Jobs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s.runJob(ctx, campaign, idx, localImagePath)
		}(i)
	}
	wg.Wait()

	s.mu.Lock()
	allDone := true
	for _, job := range campaign.Jobs {
		if job.Status != "done" && job.Status != "failed" {
			allDone = false
			break
		}
	}
	if allDone {
		campaign.Status = "done"
	}
	campaign.UpdatedAt = time.Now()
	snapshot := clonePostCampaign(campaign)
	s.mu.Unlock()
	s.publishCampaign(&snapshot, "upsert")

	if s.log != nil {
		s.log.Info("post campaign finished", "campaignId", campaign.ID)
	}
}

func (s *PostCampaignService) runJob(ctx context.Context, campaign *domain.PostCampaign, idx int, localImagePath string) {
	job := campaign.Jobs[idx]
	s.updateJob(campaign, idx, func(job *domain.PostJob) {
		job.Status = "running"
		job.Error = ""
	})

	req := PostJobRequest{
		CampaignID:     campaign.ID,
		JobID:          job.ID,
		AccountID:      job.AccountID,
		DeviceID:       domain.DeviceID(job.DeviceID),
		LocalImagePath: localImagePath,
		RemoteImageDir: "/sdcard/DCIM/ppp_posts",
		Caption:        campaign.Caption,
		WorkflowName:   "instagram-post-script",
	}
	deps := PostJobDeps{Tasks: s.tasks, Adb: s.adb}

	taskID, err := ExecutePostJob(ctx, req, deps, s.log)
	if taskID != "" {
		s.updateJob(campaign, idx, func(job *domain.PostJob) { job.TaskID = string(taskID) })
	}
	if err != nil {
		s.updateJob(campaign, idx, func(job *domain.PostJob) {
			job.Status = "failed"
			job.Error = err.Error()
		})
		if s.log != nil {
			s.log.Warn("post job failed", "campaignId", campaign.ID, "jobId", campaign.Jobs[idx].ID, "reason", err.Error())
		}
		return
	}

	s.updateJob(campaign, idx, func(job *domain.PostJob) {
		job.Status = "done"
		job.Error = ""
	})
	if s.log != nil {
		s.log.Info("post job done", "campaignId", campaign.ID, "jobId", job.ID, "accountId", job.AccountID)
	}
}

func (s *PostCampaignService) updateJob(campaign *domain.PostCampaign, idx int, fn func(*domain.PostJob)) {
	s.mu.Lock()
	fn(&campaign.Jobs[idx])
	campaign.UpdatedAt = time.Now()
	snapshot := clonePostCampaign(campaign)
	s.mu.Unlock()
	s.publishCampaign(&snapshot, "upsert")
}

func clonePostCampaign(campaign *domain.PostCampaign) domain.PostCampaign {
	if campaign == nil {
		return domain.PostCampaign{}
	}
	cp := *campaign
	cp.Jobs = append([]domain.PostJob(nil), campaign.Jobs...)
	return cp
}

func (s *PostCampaignService) publishCampaign(campaign *domain.PostCampaign, eventType string) {
	if s.publish == nil || campaign == nil {
		return
	}
	s.publish.PublishProjection(ProjectionEvent{
		Topic:      "campaigns.posts",
		Type:       eventType,
		EntityID:   campaign.ID,
		OccurredAt: time.Now().UTC(),
		Payload:    clonePostCampaign(campaign),
	})
}
