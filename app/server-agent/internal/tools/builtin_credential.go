package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const (
	passwordLower   = "abcdefghjkmnpqrstuvwxyz"
	passwordUpper   = "ABCDEFGHJKMNPQRSTUVWXYZ"
	passwordDigits  = "23456789"
	passwordSymbols = "!@#$%^&*"
)

type generatePasswordParams struct {
	Length         int   `json:"length,omitempty"`
	IncludeSymbols *bool `json:"includeSymbols,omitempty"`
}

func (p generatePasswordParams) includeSymbols() bool {
	if p.IncludeSymbols == nil {
		return true
	}
	return *p.IncludeSymbols
}

type generatedPasswordResult struct {
	Password  string `json:"password"`
	Length    int    `json:"length"`
	HasSymbol bool   `json:"hasSymbol"`
}

func generatePasswordTool() ToolDefinition {
	return ToolDefinition{
		Manifest: ToolManifest{
			Name:          "credential.generate_password",
			Description:   "Generates a strong local password",
			Deterministic: false,
			Timeout:       250 * time.Millisecond,
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{
					"length":{"type":"integer"},
					"includeSymbols":{"type":"boolean"}
				}
			}`),
			OutputSchema: rawSchema(`{
				"type":"object",
				"required":["password","length","hasSymbol"],
				"properties":{
					"password":{"type":"string"},
					"length":{"type":"integer"},
					"hasSymbol":{"type":"boolean"}
				}
			}`),
		},
		ValidateParams: validateGeneratePasswordParams,
		ValidateResult: validateGeneratedPasswordResult,
		Handler: func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			params, err := parseGeneratePasswordParams(raw)
			if err != nil {
				return nil, err
			}
			password, err := generatePassword(params.Length, params.includeSymbols())
			if err != nil {
				return nil, err
			}
			return marshalResult(generatedPasswordResult{
				Password:  password,
				Length:    len(password),
				HasSymbol: containsAny(password, passwordSymbols),
			})
		},
	}
}

func validateGeneratePasswordParams(raw json.RawMessage) error {
	_, err := parseGeneratePasswordParams(raw)
	return err
}

func parseGeneratePasswordParams(raw json.RawMessage) (generatePasswordParams, error) {
	params := generatePasswordParams{Length: 16}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return params, fmt.Errorf("decode params: %w", err)
		}
	}
	if params.Length == 0 {
		params.Length = 16
	}
	if params.Length < 12 || params.Length > 64 {
		return params, fmt.Errorf("length must be between 12 and 64")
	}
	return params, nil
}

func validateGeneratedPasswordResult(raw json.RawMessage) error {
	var result generatedPasswordResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	if result.Password == "" {
		return fmt.Errorf("password is required")
	}
	if result.Length != len(result.Password) {
		return fmt.Errorf("length must match password length")
	}
	if !containsAny(result.Password, passwordLower) || !containsAny(result.Password, passwordUpper) || !containsAny(result.Password, passwordDigits) {
		return fmt.Errorf("password must include upper, lower, and digit characters")
	}
	if result.HasSymbol != containsAny(result.Password, passwordSymbols) {
		return fmt.Errorf("hasSymbol must match generated password")
	}
	return nil
}

func generatePassword(length int, includeSymbols bool) (string, error) {
	required := []byte{}
	all := passwordLower + passwordUpper + passwordDigits

	lower, err := randomChar(passwordLower)
	if err != nil {
		return "", err
	}
	upper, err := randomChar(passwordUpper)
	if err != nil {
		return "", err
	}
	digit, err := randomChar(passwordDigits)
	if err != nil {
		return "", err
	}
	required = append(required, lower, upper, digit)

	if includeSymbols {
		symbol, err := randomChar(passwordSymbols)
		if err != nil {
			return "", err
		}
		required = append(required, symbol)
		all += passwordSymbols
	}
	if length < len(required) {
		return "", fmt.Errorf("length must be at least %d", len(required))
	}

	password := append([]byte{}, required...)
	for len(password) < length {
		c, err := randomChar(all)
		if err != nil {
			return "", err
		}
		password = append(password, c)
	}
	if err := shuffleBytes(password); err != nil {
		return "", err
	}
	return string(password), nil
}

