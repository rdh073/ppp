package loader

import (
	"encoding/json"

	"github.com/autosdk/ppp/server-agent/internal/tools"
)

// YAML struct types for providers.yaml and manifest/binding files.

type providerCatalogFile struct {
	Providers []providerConfig `yaml:"providers"`
}

type providerConfig struct {
	ID            string `yaml:"id"`
	Kind          string `yaml:"kind"`
	Optional      bool   `yaml:"optional"`
	BaseURL       string `yaml:"baseURL"`
	BaseURLEnv    string `yaml:"baseURLEnv"`
	APIURL        string `yaml:"apiURL"`
	APIURLEnv     string `yaml:"apiURLEnv"`
	APIKeyEnv     string `yaml:"apiKeyEnv"`
	Model         string `yaml:"model"`
	ModelEnv      string `yaml:"modelEnv"`
	APIVersion    string `yaml:"apiVersion"`
	APIVersionEnv string `yaml:"apiVersionEnv"`
	Timeout       string `yaml:"timeout"`
}

// manifestChainEntry is one link in a manifest-level provider chain.
type manifestChainEntry struct {
	Provider         string `yaml:"provider"`
	ProviderToolName string `yaml:"providerToolName"`
}

// catalogChainEntry is the normalized form of manifestChainEntry.
type catalogChainEntry struct {
	ProviderID       string
	ProviderToolName string
}

type toolManifestConfig struct {
	Name             string               `yaml:"name"`
	Provider         string               `yaml:"provider"`
	ProviderToolName string               `yaml:"providerToolName"`
	Providers        []manifestChainEntry `yaml:"providers"`
	Fallback         string               `yaml:"fallback"`
	Description      string               `yaml:"description"`
	Deterministic    *bool                `yaml:"deterministic"`
	Timeout          string               `yaml:"timeout"`
	RetryBudget      *int                 `yaml:"retryBudget"`
	InputSchema      any                  `yaml:"inputSchema"`
	InputSchemaFile  string               `yaml:"inputSchemaFile"`
	OutputSchema     any                  `yaml:"outputSchema"`
	OutputSchemaFile string               `yaml:"outputSchemaFile"`
	PromptFile       string               `yaml:"promptFile"`
	SystemPromptFile string               `yaml:"systemPromptFile"`
	ModelPolicy      string               `yaml:"modelPolicy"`
	Tags             []string             `yaml:"tags"`
}

type bindingCatalogFile struct {
	Bindings []bindingConfig `yaml:"bindings"`
}

type bindingConfig struct {
	ID               string                  `yaml:"id"`
	Tool             string                  `yaml:"tool"`
	Optional         bool                    `yaml:"optional"`
	Constants        map[string]any          `yaml:"constants"`
	ParamsTemplate   string                  `yaml:"paramsTemplate"`
	SuccessArtifacts []bindingArtifactConfig `yaml:"successArtifacts"`
	FailureArtifacts []bindingArtifactConfig `yaml:"failureArtifacts"`
}

type bindingArtifactConfig struct {
	Artifact        string `yaml:"artifact"`
	FromJSONPointer string `yaml:"fromJsonPointer"`
	Template        string `yaml:"template"`
	Value           any    `yaml:"value"`
}

type catalogToolManifest struct {
	Manifest       tools.ToolManifest
	PromptTemplate string
	SystemPrompt   string
	ModelPolicy    string
	ProviderChain  []catalogChainEntry
	ChainFallback  string
}

// remoteProviderTool is the JSON shape for one tool in an HTTP provider's list.
type remoteProviderTool struct {
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Deterministic *bool           `json:"deterministic,omitempty"`
	Timeout       string          `json:"timeout,omitempty"`
	RetryBudget   *int            `json:"retryBudget,omitempty"`
	InputSchema   json.RawMessage `json:"inputSchema,omitempty"`
	OutputSchema  json.RawMessage `json:"outputSchema,omitempty"`
}

type remoteProviderListEnvelope struct {
	Items []remoteProviderTool `json:"items"`
}

type remoteInvokeRequest struct {
	CallID string          `json:"callId"`
	Params json.RawMessage `json:"params"`
}

type remoteInvokeResponse struct {
	Result json.RawMessage    `json:"result,omitempty"`
	Error  *remoteInvokeError `json:"error,omitempty"`
}

type remoteInvokeError struct {
	Code      string `json:"code,omitempty"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}
