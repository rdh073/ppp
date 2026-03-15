package tools_test

import (
	"context"
	"encoding/json"
	"errors"
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
		"captcha.squares_to_taps",
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

func TestSquaresToTaps_SingleSquare(t *testing.T) {
	registry := tools.NewLocalToolRegistry()

	// 4x4 grid, bounds [0,0,400,400] → each cell is 100x100.
	// Square 2 (row=0, col=1) → center at (150, 50).
	raw, err := registry.Invoke(context.Background(), "captcha.squares_to_taps",
		json.RawMessage(`{"squares":"[2]","cols":"4","gridBounds":"[0,0,400,400]"}`))
	if err != nil {
		t.Fatal(err)
	}

	var result struct {
		Tap0  string `json:"tap0"`
		Tap1  string `json:"tap1"`
		Count string `json:"count"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Tap0 != "150,50" {
		t.Fatalf("expected tap0=150,50, got %q", result.Tap0)
	}
	if result.Tap1 != "" {
		t.Fatalf("expected tap1 to be empty, got %q", result.Tap1)
	}
	if result.Count != "1" {
		t.Fatalf("expected count=1, got %q", result.Count)
	}
}

func TestSquaresToTaps_MultipleSquares(t *testing.T) {
	registry := tools.NewLocalToolRegistry()

	// 4x4 grid, bounds [0,0,400,400] → each cell is 100x100.
	// Square 1: row=0,col=0 → (50,50)
	// Square 6: row=1,col=1 → (150,150)
	// Square 16: row=3,col=3 → (350,350)
	raw, err := registry.Invoke(context.Background(), "captcha.squares_to_taps",
		json.RawMessage(`{"squares":"[1,6,16]","cols":"4","gridBounds":"[0,0,400,400]"}`))
	if err != nil {
		t.Fatal(err)
	}

	var result struct {
		Tap0  string `json:"tap0"`
		Tap1  string `json:"tap1"`
		Tap2  string `json:"tap2"`
		Tap3  string `json:"tap3"`
		Count string `json:"count"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Tap0 != "50,50" {
		t.Fatalf("expected tap0=50,50, got %q", result.Tap0)
	}
	if result.Tap1 != "150,150" {
		t.Fatalf("expected tap1=150,150, got %q", result.Tap1)
	}
	if result.Tap2 != "350,350" {
		t.Fatalf("expected tap2=350,350, got %q", result.Tap2)
	}
	if result.Tap3 != "" {
		t.Fatalf("expected tap3 to be empty, got %q", result.Tap3)
	}
	if result.Count != "3" {
		t.Fatalf("expected count=3, got %q", result.Count)
	}
}

func TestSquaresToTaps_All16Squares(t *testing.T) {
	registry := tools.NewLocalToolRegistry()

	// 4x4 grid [0,0,400,400] — all 16 squares, each cell 100x100.
	// Square N: row=(N-1)/4, col=(N-1)%4 → center at (col*100+50, row*100+50).
	raw, err := registry.Invoke(context.Background(), "captcha.squares_to_taps",
		json.RawMessage(`{"squares":"[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16]","cols":"4","gridBounds":"[0,0,400,400]"}`))
	if err != nil {
		t.Fatal(err)
	}

	var result struct {
		Tap0  string `json:"tap0"`
		Tap15 string `json:"tap15"`
		Count string `json:"count"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Tap0 != "50,50" {
		t.Fatalf("expected tap0=50,50, got %q", result.Tap0)
	}
	// Square 16: row=3,col=3 → center (350,350).
	if result.Tap15 != "350,350" {
		t.Fatalf("expected tap15=350,350, got %q", result.Tap15)
	}
	if result.Count != "16" {
		t.Fatalf("expected count=16, got %q", result.Count)
	}
}

func TestSquaresToTaps_DefaultCols(t *testing.T) {
	registry := tools.NewLocalToolRegistry()

	// No cols → defaults to 4; 4x4 grid, bounds [0,0,400,400].
	// Square 5: row=1,col=0 → (50,150).
	raw, err := registry.Invoke(context.Background(), "captcha.squares_to_taps",
		json.RawMessage(`{"squares":"[5]","gridBounds":"[0,0,400,400]"}`))
	if err != nil {
		t.Fatal(err)
	}

	var result struct {
		Tap0 string `json:"tap0"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Tap0 != "50,150" {
		t.Fatalf("expected tap0=50,150, got %q", result.Tap0)
	}
}

func TestSquaresToTaps_InvalidParams(t *testing.T) {
	registry := tools.NewLocalToolRegistry()

	cases := []struct {
		name   string
		params string
	}{
		{"missing squares", `{"gridBounds":"[0,0,400,400]"}`},
		{"missing gridBounds", `{"squares":"[1]"}`},
		{"bad squares json", `{"squares":"not-json","gridBounds":"[0,0,400,400]"}`},
		{"zero square", `{"squares":"[0]","gridBounds":"[0,0,400,400]"}`},
		{"bad gridBounds", `{"squares":"[1]","gridBounds":"[0,0,100]"}`},
		{"inverted bounds", `{"squares":"[1]","gridBounds":"[400,400,0,0]"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := registry.Invoke(context.Background(), "captcha.squares_to_taps",
				json.RawMessage(tc.params))
			if !errors.Is(err, tools.ErrToolInvalidParams) {
				t.Fatalf("expected ErrToolInvalidParams, got %v", err)
			}
		})
	}
}
