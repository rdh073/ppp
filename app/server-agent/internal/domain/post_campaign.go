package domain

import "time"

// PostJob tracks a single per-account post job within a PostCampaign.
type PostJob struct {
	ID        string `json:"id"`
	AccountID string `json:"accountId"`
	DeviceID  string `json:"deviceId"`
	TaskID    string `json:"taskId,omitempty"`
	Status    string `json:"status"` // pending|running|done|failed
	Error     string `json:"error,omitempty"`
}

// PostCampaign tracks a batch Instagram post campaign across multiple accounts.
type PostCampaign struct {
	ID          string    `json:"id"`
	ImageSource string    `json:"imageSource"` // "manual"|"ai"
	TextSource  string    `json:"textSource"`  // "manual"|"ai"
	Caption     string    `json:"caption"`
	Jobs        []PostJob `json:"jobs"`
	Status      string    `json:"status"` // running|done|failed
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func NewPostCampaignID() string {
	return "post-" + newID()
}

func NewPostJobID() string {
	return "pjob-" + newID()
}
