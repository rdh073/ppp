package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type schemaValidator func(json.RawMessage) error

type simpleSchema struct {
	Type       string                   `json:"type"`
	Required   []string                 `json:"required"`
	Enum       []any                    `json:"enum"`
	Properties map[string]*simpleSchema `json:"properties"`
	Items      *simpleSchema            `json:"items"`
	Minimum    *float64                 `json:"minimum"`
	Maximum    *float64                 `json:"maximum"`
	MinLength  *int                     `json:"minLength"`
	MaxLength  *int                     `json:"maxLength"`
}

func compileSchemaValidator(raw json.RawMessage) (schemaValidator, error) {
	raw = normalizeJSON(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	var schema simpleSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("decode schema: %w", err)
	}
	if err := validateSchemaDefinition(&schema, "$"); err != nil {
		return nil, err
	}
	return func(payload json.RawMessage) error {
		if len(payload) == 0 {
			payload = json.RawMessage(`null`)
		}
		value, err := decodeJSONValue(payload)
		if err != nil {
			return err
		}
		return validateSchemaValue(&schema, value, "$")
	}, nil
}

func decodeJSONValue(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	return value, nil
}

func validateSchemaDefinition(schema *simpleSchema, path string) error {
	if schema == nil {
		return fmt.Errorf("schema %s is nil", path)
	}
	switch schema.Type {
	case "", "object", "string", "integer", "number", "boolean", "array":
	default:
		return fmt.Errorf("schema %s has unsupported type %q", path, schema.Type)
	}
	for _, item := range schema.Required {
		if strings.TrimSpace(item) == "" {
			return fmt.Errorf("schema %s has empty required key", path)
		}
	}
	for name, child := range schema.Properties {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("schema %s has empty property name", path)
		}
		if err := validateSchemaDefinition(child, path+"."+name); err != nil {
			return err
		}
	}
	if schema.Items != nil {
		if err := validateSchemaDefinition(schema.Items, path+"[]"); err != nil {
			return err
		}
	}
	if schema.Type == "object" && schema.Properties == nil {
		schema.Properties = map[string]*simpleSchema{}
	}
	if schema.Type == "array" && schema.Items == nil {
		return fmt.Errorf("schema %s array type requires items", path)
	}
	return nil
}

func validateSchemaValue(schema *simpleSchema, value any, path string) error {
	if schema == nil {
		return fmt.Errorf("schema %s is nil", path)
	}
	if len(schema.Enum) > 0 {
		matched := false
		for _, candidate := range schema.Enum {
			if sameJSONScalar(candidate, value) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s must be one of %v", path, schema.Enum)
		}
	}

	switch schema.Type {
	case "":
		return nil
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be object", path)
		}
		for _, key := range schema.Required {
			if _, exists := obj[key]; !exists {
				return fmt.Errorf("%s missing required property %q", path, key)
			}
		}
		for name, child := range schema.Properties {
			childValue, exists := obj[name]
			if !exists {
				continue
			}
			if err := validateSchemaValue(child, childValue, path+"."+name); err != nil {
				return err
			}
		}
		return nil
	case "string":
		str, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be string", path)
		}
		if schema.MinLength != nil && len(str) < *schema.MinLength {
			return fmt.Errorf("%s length must be >= %d", path, *schema.MinLength)
		}
		if schema.MaxLength != nil && len(str) > *schema.MaxLength {
			return fmt.Errorf("%s length must be <= %d", path, *schema.MaxLength)
		}
		return nil
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be boolean", path)
		}
		return nil
	case "integer":
		number, ok := asFloat(value)
		if !ok || math.Trunc(number) != number {
			return fmt.Errorf("%s must be integer", path)
		}
		if schema.Minimum != nil && number < *schema.Minimum {
			return fmt.Errorf("%s must be >= %v", path, *schema.Minimum)
		}
		if schema.Maximum != nil && number > *schema.Maximum {
			return fmt.Errorf("%s must be <= %v", path, *schema.Maximum)
		}
		return nil
	case "number":
		number, ok := asFloat(value)
		if !ok {
			return fmt.Errorf("%s must be number", path)
		}
		if schema.Minimum != nil && number < *schema.Minimum {
			return fmt.Errorf("%s must be >= %v", path, *schema.Minimum)
		}
		if schema.Maximum != nil && number > *schema.Maximum {
			return fmt.Errorf("%s must be <= %v", path, *schema.Maximum)
		}
		return nil
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be array", path)
		}
		for idx, item := range items {
			if err := validateSchemaValue(schema.Items, item, fmt.Sprintf("%s[%d]", path, idx)); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%s has unsupported type %q", path, schema.Type)
	}
}

func sameJSONScalar(expected any, actual any) bool {
	switch v := expected.(type) {
	case string:
		candidate, ok := actual.(string)
		return ok && candidate == v
	case bool:
		candidate, ok := actual.(bool)
		return ok && candidate == v
	case float64:
		number, ok := asFloat(actual)
		return ok && number == v
	case json.Number:
		number, err := v.Float64()
		if err != nil {
			return false
		}
		candidate, ok := asFloat(actual)
		return ok && candidate == number
	default:
		return false
	}
}

func asFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case json.Number:
		number, err := v.Float64()
		return number, err == nil
	case string:
		number, err := strconv.ParseFloat(v, 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func normalizeJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return json.RawMessage(bytes.TrimSpace(raw))
}
