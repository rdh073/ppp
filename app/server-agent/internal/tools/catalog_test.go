package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/tools"
	"github.com/autosdk/ppp/server-agent/internal/workflow/nodes"
)

func TestLocalToolRegistry_ContainsExpectedTools(t *testing.T) {
	registry := tools.NewLocalToolRegistry()

	for _, toolName := range []string{
		"identity.generate_indonesian_name",
		"identity.generate_email",
		"credential.generate_password",
		"identity.generate_birth_date",
	} {
		if _, ok := registry.Manifest(toolName); !ok {
			t.Fatalf("expected manifest for %s", toolName)
		}
	}
}

func TestGenerateIndonesianName_Invoke(t *testing.T) {
	registry := tools.NewLocalToolRegistry()

	raw, err := registry.Invoke(context.Background(), "identity.generate_indonesian_name", json.RawMessage(`{"gender":"female"}`))
	if err != nil {
		t.Fatal(err)
	}

	var result struct {
		FullName  string `json:"fullName"`
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
		Gender    string `json:"gender"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Gender != "female" {
		t.Fatalf("expected female gender, got %q", result.Gender)
	}
	if result.FullName == "" || result.FirstName == "" || result.LastName == "" {
		t.Fatal("expected generated name fields to be populated")
	}
}

func TestGenerateEmail_Invoke(t *testing.T) {
	registry := tools.NewLocalToolRegistry()

	raw, err := registry.Invoke(context.Background(), "identity.generate_email", json.RawMessage(`{"fullName":"Ayu Lestari","domain":"example.id"}`))
	if err != nil {
		t.Fatal(err)
	}

	var result struct {
		Email     string `json:"email"`
		LocalPart string `json:"localPart"`
		Domain    string `json:"domain"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.LocalPart != "ayu.lestari" {
		t.Fatalf("expected localPart ayu.lestari, got %q", result.LocalPart)
	}
	if result.Email != "ayu.lestari@example.id" {
		t.Fatalf("unexpected email: %q", result.Email)
	}
}

func TestGenerateEmail_InvalidParams(t *testing.T) {
	registry := tools.NewLocalToolRegistry()

	_, err := registry.Invoke(context.Background(), "identity.generate_email", json.RawMessage(`{}`))
	if !errors.Is(err, nodes.ErrToolInvalidParams) {
		t.Fatalf("expected ErrToolInvalidParams, got %v", err)
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
