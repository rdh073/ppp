package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const dateLayout = "2006-01-02"

type generateBirthDateParams struct {
	MinAge        int    `json:"minAge,omitempty"`
	MaxAge        int    `json:"maxAge,omitempty"`
	ReferenceDate string `json:"referenceDate,omitempty"`
}

type generatedBirthDateResult struct {
	BirthDate     string `json:"birthDate"`
	BirthMonth    int    `json:"birthMonth"`
	BirthDay      int    `json:"birthDay"`
	BirthYear     int    `json:"birthYear"`
	Age           int    `json:"age"`
	ReferenceDate string `json:"referenceDate"`
}

func generateBirthDateTool() ToolDefinition {
	return ToolDefinition{
		Manifest: ToolManifest{
			Name:          "identity.generate_birth_date",
			Description:   "Generates a local birth date within an age range",
			Deterministic: false,
			Timeout:       250 * time.Millisecond,
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{
					"minAge":{"type":"integer"},
					"maxAge":{"type":"integer"},
					"referenceDate":{"type":"string"}
				}
			}`),
			OutputSchema: rawSchema(`{
				"type":"object",
				"required":["birthDate","birthMonth","birthDay","birthYear","age","referenceDate"],
				"properties":{
					"birthDate":{"type":"string"},
					"birthMonth":{"type":"integer"},
					"birthDay":{"type":"integer"},
					"birthYear":{"type":"integer"},
					"age":{"type":"integer"},
					"referenceDate":{"type":"string"}
				}
			}`),
		},
		ValidateParams: validateGenerateBirthDateParams,
		ValidateResult: validateGeneratedBirthDateResult,
		Handler: func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			params, refDate, err := parseGenerateBirthDateParams(raw)
			if err != nil {
				return nil, err
			}
			age, err := randomIntBetween(params.MinAge, params.MaxAge)
			if err != nil {
				return nil, err
			}
			earliest := refDate.AddDate(-(age + 1), 0, 1)
			latest := refDate.AddDate(-age, 0, 0)
			days := int(latest.Sub(earliest).Hours() / 24)
			offset := 0
			if days > 0 {
				offset, err = randomIntBetween(0, days)
				if err != nil {
					return nil, err
				}
			}
			birthDate := earliest.AddDate(0, 0, offset)
			return marshalResult(generatedBirthDateResult{
				BirthDate:     birthDate.Format(dateLayout),
				BirthMonth:    int(birthDate.Month()),
				BirthDay:      birthDate.Day(),
				BirthYear:     birthDate.Year(),
				Age:           calculateAge(birthDate, refDate),
				ReferenceDate: refDate.Format(dateLayout),
			})
		},
	}
}

func validateGenerateBirthDateParams(raw json.RawMessage) error {
	_, _, err := parseGenerateBirthDateParams(raw)
	return err
}

func parseGenerateBirthDateParams(raw json.RawMessage) (generateBirthDateParams, time.Time, error) {
	params := generateBirthDateParams{
		MinAge: 21,
		MaxAge: 40,
	}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return params, time.Time{}, fmt.Errorf("decode params: %w", err)
		}
	}
	if params.MinAge == 0 {
		params.MinAge = 21
	}
	if params.MaxAge == 0 {
		params.MaxAge = 40
	}
	if params.MinAge < 18 {
		return params, time.Time{}, fmt.Errorf("minAge must be at least 18")
	}
	if params.MaxAge < params.MinAge {
		return params, time.Time{}, fmt.Errorf("maxAge must be greater than or equal to minAge")
	}

	refDate := time.Now().UTC()
	if params.ReferenceDate != "" {
		parsed, err := time.Parse(dateLayout, params.ReferenceDate)
		if err != nil {
			return params, time.Time{}, fmt.Errorf("referenceDate must use %s", dateLayout)
		}
		refDate = parsed.UTC()
	}
	return params, refDate, nil
}

func validateGeneratedBirthDateResult(raw json.RawMessage) error {
	var result generatedBirthDateResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	if result.BirthDate == "" || result.ReferenceDate == "" {
		return fmt.Errorf("birthDate and referenceDate are required")
	}
	birthDate, err := time.Parse(dateLayout, result.BirthDate)
	if err != nil {
		return fmt.Errorf("birthDate must use %s", dateLayout)
	}
	refDate, err := time.Parse(dateLayout, result.ReferenceDate)
	if err != nil {
		return fmt.Errorf("referenceDate must use %s", dateLayout)
	}
	if calculateAge(birthDate, refDate) != result.Age {
		return fmt.Errorf("age does not match birthDate and referenceDate")
	}
	if result.BirthMonth != int(birthDate.Month()) ||
		result.BirthDay != birthDate.Day() ||
		result.BirthYear != birthDate.Year() {
		return fmt.Errorf("birthMonth/Day/Year do not match birthDate")
	}
	return nil
}

func calculateAge(birthDate, referenceDate time.Time) int {
	age := referenceDate.Year() - birthDate.Year()
	if referenceDate.Month() < birthDate.Month() ||
		(referenceDate.Month() == birthDate.Month() && referenceDate.Day() < birthDate.Day()) {
		age--
	}
	return age
}
