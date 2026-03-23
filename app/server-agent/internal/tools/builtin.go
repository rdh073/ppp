package tools

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// LocalToolDefinitions returns all built-in deterministic tool implementations.
// These run in-process with no external dependencies.
func LocalToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		generatePasswordTool(),
		generateBirthDateTool(),
	}
}

func NewLocalToolRegistry() StaticToolRegistry {
	return NewStaticToolRegistry(LocalToolDefinitions()...)
}

// --- shared helpers ---

func marshalResult(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

func rawSchema(s string) json.RawMessage {
	return json.RawMessage(s)
}

func chooseOne(values []string) (string, error) {
	if len(values) == 0 {
		return "", fmt.Errorf("values must not be empty")
	}
	idx, err := randomIntBetween(0, len(values)-1)
	if err != nil {
		return "", err
	}
	return values[idx], nil
}

func randomIntBetween(min, max int) (int, error) {
	if max < min {
		return 0, fmt.Errorf("invalid range")
	}
	n := max - min + 1
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(v.Int64()) + min, nil
}

func randomChar(alphabet string) (byte, error) {
	idx, err := randomIntBetween(0, len(alphabet)-1)
	if err != nil {
		return 0, err
	}
	return alphabet[idx], nil
}

func shuffleBytes(values []byte) error {
	for i := len(values) - 1; i > 0; i-- {
		j, err := randomIntBetween(0, i)
		if err != nil {
			return err
		}
		values[i], values[j] = values[j], values[i]
	}
	return nil
}

func containsAny(value, alphabet string) bool {
	for _, r := range value {
		if strings.ContainsRune(alphabet, r) {
			return true
		}
	}
	return false
}
