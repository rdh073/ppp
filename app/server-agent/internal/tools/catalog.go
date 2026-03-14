package tools

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/workflow/nodes"
)

const (
	defaultEmailDomain = "mailnesia.id"
	dateLayout         = "2006-01-02"
)

var (
	maleFirstNames = []string{
		"Adi", "Arif", "Bagas", "Bima", "Dimas",
		"Fajar", "Raka", "Rizky", "Wahyu", "Yudha",
	}
	femaleFirstNames = []string{
		"Aulia", "Ayu", "Citra", "Dewi", "Indah",
		"Laras", "Nabila", "Putri", "Rani", "Sari",
	}
	lastNames = []string{
		"Hidayat", "Kusuma", "Lestari", "Mahendra", "Permana",
		"Pratama", "Purnama", "Ramadhan", "Saputra", "Wijaya",
	}
)

type generateNameParams struct {
	Gender string `json:"gender,omitempty"`
}

type generatedNameResult struct {
	FullName  string `json:"fullName"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Gender    string `json:"gender"`
}

type generateEmailParams struct {
	FullName  string `json:"fullName,omitempty"`
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
	Domain    string `json:"domain,omitempty"`
}

type generatedEmailResult struct {
	Email     string `json:"email"`
	LocalPart string `json:"localPart"`
	Domain    string `json:"domain"`
}

type generatePasswordParams struct {
	Length         int   `json:"length,omitempty"`
	IncludeSymbols *bool `json:"includeSymbols,omitempty"`
}

type generatedPasswordResult struct {
	Password  string `json:"password"`
	Length    int    `json:"length"`
	HasSymbol bool   `json:"hasSymbol"`
}

type generateBirthDateParams struct {
	MinAge        int    `json:"minAge,omitempty"`
	MaxAge        int    `json:"maxAge,omitempty"`
	ReferenceDate string `json:"referenceDate,omitempty"`
}

type generatedBirthDateResult struct {
	BirthDate     string `json:"birthDate"`
	Age           int    `json:"age"`
	ReferenceDate string `json:"referenceDate"`
}

func LocalToolDefinitions() []nodes.ToolDefinition {
	return []nodes.ToolDefinition{
		generateIndonesianNameTool(),
		generateEmailTool(),
		generatePasswordTool(),
		generateBirthDateTool(),
	}
}

func NewLocalToolRegistry() nodes.StaticToolRegistry {
	return nodes.NewStaticToolRegistry(LocalToolDefinitions()...)
}

func generateIndonesianNameTool() nodes.ToolDefinition {
	return nodes.ToolDefinition{
		Manifest: nodes.ToolManifest{
			Name:          "identity.generate_indonesian_name",
			Description:   "Generates a local Indonesian-style full name",
			Deterministic: false,
			Timeout:       250 * time.Millisecond,
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{"gender":{"enum":["male","female"]}}
			}`),
			OutputSchema: rawSchema(`{
				"type":"object",
				"required":["fullName","firstName","lastName","gender"],
				"properties":{
					"fullName":{"type":"string"},
					"firstName":{"type":"string"},
					"lastName":{"type":"string"},
					"gender":{"enum":["male","female"]}
				}
			}`),
		},
		ValidateParams: validateGenerateNameParams,
		ValidateResult: validateGeneratedNameResult,
		Handler: func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			params, err := parseGenerateNameParams(raw)
			if err != nil {
				return nil, err
			}
			gender := params.Gender
			if gender == "" {
				gender, err = chooseOne([]string{"male", "female"})
				if err != nil {
					return nil, err
				}
			}

			firstPool := maleFirstNames
			if gender == "female" {
				firstPool = femaleFirstNames
			}
			firstName, err := chooseOne(firstPool)
			if err != nil {
				return nil, err
			}
			lastName, err := chooseOne(lastNames)
			if err != nil {
				return nil, err
			}

			return marshalResult(generatedNameResult{
				FullName:  strings.TrimSpace(firstName + " " + lastName),
				FirstName: firstName,
				LastName:  lastName,
				Gender:    gender,
			})
		},
	}
}

func generateEmailTool() nodes.ToolDefinition {
	return nodes.ToolDefinition{
		Manifest: nodes.ToolManifest{
			Name:          "identity.generate_email",
			Description:   "Composes an email address from a known name",
			Deterministic: true,
			Timeout:       250 * time.Millisecond,
			InputSchema: rawSchema(`{
				"type":"object",
				"required":["fullName"],
				"properties":{
					"fullName":{"type":"string"},
					"domain":{"type":"string"}
				}
			}`),
			OutputSchema: rawSchema(`{
				"type":"object",
				"required":["email","localPart","domain"],
				"properties":{
					"email":{"type":"string"},
					"localPart":{"type":"string"},
					"domain":{"type":"string"}
				}
			}`),
		},
		ValidateParams: validateGenerateEmailParams,
		ValidateResult: validateGeneratedEmailResult,
		Handler: func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			params, err := parseGenerateEmailParams(raw)
			if err != nil {
				return nil, err
			}
			fullName := strings.TrimSpace(params.FullName)
			if fullName == "" {
				fullName = strings.TrimSpace(strings.TrimSpace(params.FirstName) + " " + strings.TrimSpace(params.LastName))
			}
			localPart := emailLocalPart(fullName)
			domain := defaultEmailDomain
			if params.Domain != "" {
				domain = strings.ToLower(strings.TrimSpace(params.Domain))
			}

			return marshalResult(generatedEmailResult{
				Email:     localPart + "@" + domain,
				LocalPart: localPart,
				Domain:    domain,
			})
		},
	}
}

func generatePasswordTool() nodes.ToolDefinition {
	return nodes.ToolDefinition{
		Manifest: nodes.ToolManifest{
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

func generateBirthDateTool() nodes.ToolDefinition {
	return nodes.ToolDefinition{
		Manifest: nodes.ToolManifest{
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
				"required":["birthDate","age","referenceDate"],
				"properties":{
					"birthDate":{"type":"string"},
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
				Age:           calculateAge(birthDate, refDate),
				ReferenceDate: refDate.Format(dateLayout),
			})
		},
	}
}

func validateGenerateNameParams(raw json.RawMessage) error {
	_, err := parseGenerateNameParams(raw)
	return err
}

func parseGenerateNameParams(raw json.RawMessage) (generateNameParams, error) {
	var params generateNameParams
	if len(raw) == 0 {
		return params, nil
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return params, fmt.Errorf("decode params: %w", err)
	}
	switch params.Gender {
	case "", "male", "female":
		return params, nil
	default:
		return params, fmt.Errorf("gender must be one of male or female")
	}
}

func validateGeneratedNameResult(raw json.RawMessage) error {
	var result generatedNameResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	if strings.TrimSpace(result.FullName) == "" || strings.TrimSpace(result.FirstName) == "" || strings.TrimSpace(result.LastName) == "" {
		return fmt.Errorf("fullName, firstName, and lastName are required")
	}
	switch result.Gender {
	case "male", "female":
		return nil
	default:
		return fmt.Errorf("gender must be one of male or female")
	}
}

func validateGenerateEmailParams(raw json.RawMessage) error {
	_, err := parseGenerateEmailParams(raw)
	return err
}

func parseGenerateEmailParams(raw json.RawMessage) (generateEmailParams, error) {
	var params generateEmailParams
	if len(raw) == 0 {
		return params, fmt.Errorf("fullName or firstName is required")
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return params, fmt.Errorf("decode params: %w", err)
	}
	fullName := strings.TrimSpace(params.FullName)
	firstName := strings.TrimSpace(params.FirstName)
	if fullName == "" && firstName == "" {
		return params, fmt.Errorf("fullName or firstName is required")
	}
	if params.Domain != "" && !isValidDomain(params.Domain) {
		return params, fmt.Errorf("domain must be a simple hostname")
	}
	return params, nil
}

func validateGeneratedEmailResult(raw json.RawMessage) error {
	var result generatedEmailResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	if result.LocalPart == "" || result.Domain == "" || result.Email == "" {
		return fmt.Errorf("email, localPart, and domain are required")
	}
	if result.Email != result.LocalPart+"@"+result.Domain {
		return fmt.Errorf("email must match localPart@domain")
	}
	if !isValidDomain(result.Domain) {
		return fmt.Errorf("domain must be a simple hostname")
	}
	return nil
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

func (p generatePasswordParams) includeSymbols() bool {
	if p.IncludeSymbols == nil {
		return true
	}
	return *p.IncludeSymbols
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
	return nil
}

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

func emailLocalPart(name string) string {
	var b strings.Builder
	prevDot := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			prevDot = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDot = false
		case r == ' ' || r == '-' || r == '_' || r == '.':
			if b.Len() > 0 && !prevDot {
				b.WriteByte('.')
				prevDot = true
			}
		}
	}
	localPart := strings.Trim(b.String(), ".")
	if localPart == "" {
		return "user"
	}
	return localPart
}

func isValidDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || !strings.Contains(domain, ".") {
		return false
	}
	for _, r := range domain {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '-':
		default:
			return false
		}
	}
	return true
}

const (
	passwordLower   = "abcdefghjkmnpqrstuvwxyz"
	passwordUpper   = "ABCDEFGHJKMNPQRSTUVWXYZ"
	passwordDigits  = "23456789"
	passwordSymbols = "!@#$%^&*"
)

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

func calculateAge(birthDate, referenceDate time.Time) int {
	age := referenceDate.Year() - birthDate.Year()
	if referenceDate.Month() < birthDate.Month() ||
		(referenceDate.Month() == birthDate.Month() && referenceDate.Day() < birthDate.Day()) {
		age--
	}
	return age
}
