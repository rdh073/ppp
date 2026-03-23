package main

import "testing"

func TestWireCaptionGenerator_ReturnsErrorWhenTextGenerationIsUnconfigured(t *testing.T) {
	cfg := DefaultConfig()

	gen, err := wireCaptionGenerator(cfg)
	if err == nil {
		t.Fatal("expected error for unconfigured text generation")
	}
	if gen != nil {
		t.Fatal("expected nil generator for unconfigured text generation")
	}
}

func TestWireCaptionGenerator_ReturnsGeneratorWhenProviderHasAPIKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LLM.Providers = map[string]LLMProviderConfig{
		"openai": {
			APIURL: "https://api.openai.com/v1/chat/completions",
			APIKey: "test-key",
			Model:  "gpt-4o-mini",
		},
	}
	cfg.LLM.TextGeneration = LLMConcernConfig{Provider: "openai"}

	gen, err := wireCaptionGenerator(cfg)
	if err != nil {
		t.Fatalf("wireCaptionGenerator() error = %v", err)
	}
	if gen == nil {
		t.Fatal("expected non-nil generator")
	}
}
