package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/tools"
)

func TestLocalToolRegistry_ContainsExpectedTools(t *testing.T) {
	registry := tools.NewLocalToolRegistry()

	for _, toolName := range []string{
		"credential.generate_password",
		"identity.generate_birth_date",
	} {
		if _, ok := registry.Manifest(toolName); !ok {
			t.Fatalf("expected manifest for %s", toolName)
		}
	}
}

func TestLocalToolRegistry_RemovedTools(t *testing.T) {
	registry := tools.NewLocalToolRegistry()

	for _, toolName := range []string{
		"identity.generate_indonesian_name",
		"identity.generate_email",
		"captcha.squares_to_taps",
	} {
		if _, ok := registry.Manifest(toolName); ok {
			t.Fatalf("tool %s should have been removed (now LLM-backed)", toolName)
		}
	}
}


func TestGeneratePassword_Invoke(t *testing.T) {
	registry := tools.NewLocalToolRegistry()

	raw, err := registry.Invoke(context.Background(), "credential.generate_password", json.RawMessage(`{"length":20,"includeSymbols":true}`))
	if err != nil {
		t.Fatal(err)
	}

	var result struct {
		Password  string `json:"password"`
		Length    int    `json:"length"`
		HasSymbol bool   `json:"hasSymbol"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Length != 20 || len(result.Password) != 20 {
		t.Fatalf("expected 20-char password, got len field=%d actual=%d", result.Length, len(result.Password))
	}
	if !result.HasSymbol {
		t.Fatal("expected generated password to include a symbol")
	}
	if !strings.ContainsAny(result.Password, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Fatal("expected uppercase in password")
	}
	if !strings.ContainsAny(result.Password, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatal("expected lowercase in password")
	}
	if !strings.ContainsAny(result.Password, "0123456789") {
		t.Fatal("expected digit in password")
	}
}

func TestGenerateBirthDate_Invoke(t *testing.T) {
	registry := tools.NewLocalToolRegistry()

	raw, err := registry.Invoke(context.Background(), "identity.generate_birth_date", json.RawMessage(`{"minAge":25,"maxAge":25,"referenceDate":"2026-03-14"}`))
	if err != nil {
		t.Fatal(err)
	}

	var result struct {
		BirthDate     string `json:"birthDate"`
		Age           int    `json:"age"`
		ReferenceDate string `json:"referenceDate"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Age != 25 {
		t.Fatalf("expected age 25, got %d", result.Age)
	}
	birthDate, err := time.Parse("2006-01-02", result.BirthDate)
	if err != nil {
		t.Fatal(err)
	}
	referenceDate, err := time.Parse("2006-01-02", result.ReferenceDate)
	if err != nil {
		t.Fatal(err)
	}
	if birthDate.After(referenceDate) {
		t.Fatal("birthDate must not be in the future relative to referenceDate")
	}
}

