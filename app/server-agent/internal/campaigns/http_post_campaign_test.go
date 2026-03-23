package campaigns

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

func TestPostCampaignHandler_Capabilities(t *testing.T) {
	dir := t.TempDir()
	accounts, err := store.NewFileAccountStore(dir)
	if err != nil {
		t.Fatalf("NewFileAccountStore: %v", err)
	}

	service := NewPostCampaignService(newFakeTaskControl(), accounts, nil, nil, nil, nil, dir, newTestLogger())
	handler := NewPostCampaignHandler(service, "/campaigns/posts")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/campaigns/posts/capabilities", nil)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload PostCampaignCapabilities
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.ImageAI.Available {
		t.Fatal("expected image AI to be unavailable")
	}
	if payload.TextAI.Available {
		t.Fatal("expected text AI to be unavailable")
	}
	if payload.ImageAI.Reason == "" || payload.TextAI.Reason == "" {
		t.Fatalf("expected capability reasons, got %+v", payload)
	}
}

func TestPostCampaignHandler_CreateReturns503WhenTextAIUnavailable(t *testing.T) {
	dir := t.TempDir()
	accounts, err := store.NewFileAccountStore(dir)
	if err != nil {
		t.Fatalf("NewFileAccountStore: %v", err)
	}
	account := domain.Account{
		ID:        "acct-1",
		Kind:      "instagram",
		DeviceID:  "device-1",
		Status:    domain.AccountStatusActive,
		CreatedAt: time.Now(),
	}
	if err := accounts.Save(account); err != nil {
		t.Fatalf("Save account: %v", err)
	}

	service := NewPostCampaignService(newFakeTaskControl(), accounts, nil, nil, nil, nil, dir, newTestLogger())
	handler := NewPostCampaignHandler(service, "/campaigns/posts")

	body := map[string]any{
		"accountIds":  []string{account.ID},
		"imageSource": "manual",
		"imageBase64": base64.StdEncoding.EncodeToString([]byte("fake-jpeg")),
		"textSource":  "ai",
		"textPrompt":  "write a launch caption",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/campaigns/posts", bytes.NewReader(raw))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "AI caption generation is not configured") {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}
}
