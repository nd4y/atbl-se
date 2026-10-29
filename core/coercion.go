package core

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"math/big"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"autoblow.local/emulator/internal/protocol"
)

var jsDecimal = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func jsSpace(r rune) bool {
	return r == '\t' || r == '\v' || r == '\f' || r == ' ' || r == '\n' || r == '\r' || r == 0xa0 || r == 0x1680 || r >= 0x2000 && r <= 0x200a || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000 || r == 0xfeff
}

func jsNumericString(s string) float64 {
	s = strings.TrimFunc(s, jsSpace)
	if s == "" {
		return 0
	}
	if s == "Infinity" || s == "+Infinity" {
		return math.Inf(1)
	}
	if s == "-Infinity" {
		return math.Inf(-1)
	}
	if len(s) > 2 && s[0] == '0' {
		base := 0
		switch s[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			return jsRadixInteger(s[2:], base)
		}
	}
	if !jsDecimal.MatchString(s) {
		return math.NaN()
	}
	n, _ := strconv.ParseFloat(s, 64)
	return n
}

// Number validates the whole radix literal before converting; ParseUint can
// report overflow before seeing an invalid suffix. Keep precision beyond uint64.
func jsRadixInteger(s string, base int) float64 {
	if s == "" {
		return math.NaN()
	}
	for _, c := range s {
		digit := -1
		switch {
		case c >= '0' && c <= '9':
			digit = int(c - '0')
		case c >= 'a' && c <= 'f':
			digit = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			digit = int(c-'A') + 10
		}
		if digit < 0 || digit >= base {
			return math.NaN()
		}
	}
	n, ok := new(big.Int).SetString(s, base)
	if !ok {
		return math.NaN()
	}
	f, _ := n.Float64()
	return f
}

func jsString(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case json.Number:
		return x.String(), nil
	case float64:
		return jsNumberString(x), nil
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			s, err := jsString(item)
			if err != nil {
				return "", err
			}
			parts[i] = s
		}
		return strings.Join(parts, ","), nil
	case map[string]any:
		if _, ok := x["toString"]; ok {
			return "", &protocol.ConversionError{Code: "TypeError", Message: "Cannot convert object to primitive value"}
		}
		return "[object Object]", nil
	}
	return "", nil
}

// Script fields use JavaScript Number coercion, unlike REST schema coercion.
func jsNumber(v any) (float64, error) {
	switch x := v.(type) {
	case nil:
		return 0, nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case float64:
		return x, nil
	case json.Number:
		n, _ := strconv.ParseFloat(x.String(), 64)
		return n, nil
	case string:
		return jsNumericString(x), nil
	default:
		s, err := jsString(v)
		return jsNumericString(s), err
	}
}

func csvInteger(s string) float64 {
	s = strings.TrimLeftFunc(s, jsSpace)
	sign := 1.0
	if strings.HasPrefix(s, "-") {
		sign = -1
		s = s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	base := 10
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		base = 16
		s = s[2:]
	}
	end := 0
	for end < len(s) {
		c := s[end]
		if c >= '0' && c <= '9' || base == 16 && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			end++
		} else {
			break
		}
	}
	if end == 0 {
		return 0
	} // NaN becomes zero in the stock Buffer writer.
	if base == 10 {
		n, _ := strconv.ParseFloat(s[:end], 64)
		return sign * n
	}
	return sign * jsRadixInteger(s[:end], 16)
}

func jsNumberString(v float64) string {
	return protocol.NumberString(v)
}

type apiNumber struct {
	Value          float64
	Present, Valid bool
}

func decodeAPIValue(raw []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	err := d.Decode(&v)
	return v, err
}

func (n *apiNumber) UnmarshalJSON(raw []byte) error {
	*n = apiNumber{Present: true}
	v, err := decodeAPIValue(raw)
	if err != nil {
		return err
	}
	if a, ok := v.([]any); ok {
		if len(a) != 1 {
			return nil
		}
		v = a[0]
	}
	switch x := v.(type) {
	case map[string]any, []any:
		return nil
	case string:
		if x == "" {
			return nil
		}
	}
	n.Value, _ = jsNumber(v)
	n.Valid = !math.IsNaN(n.Value) && !math.IsInf(n.Value, 0)
	return nil
}

type apiBool struct {
	Value          bool
	Present, Valid bool
}

type apiString struct {
	Value          string
	Present, Valid bool
}

// Captured REST string schemas coerce scalars and unwrap one-element arrays.
// Objects and nested or non-singleton arrays remain validation errors.
func (s *apiString) UnmarshalJSON(raw []byte) error {
	*s = apiString{Present: true}
	v, err := decodeAPIValue(raw)
	if err != nil {
		return err
	}
	if a, ok := v.([]any); ok {
		if len(a) != 1 {
			return nil
		}
		v = a[0]
	}
	switch x := v.(type) {
	case nil:
		s.Value = ""
	case string:
		s.Value = x
	case bool:
		s.Value = strconv.FormatBool(x)
	case float64:
		s.Value = jsNumberString(x)
	case json.Number:
		n, _ := strconv.ParseFloat(string(x), 64)
		s.Value = jsNumberString(n)
	default:
		return nil
	}
	s.Valid = true
	return nil
}

func (b *apiBool) UnmarshalJSON(raw []byte) error {
	*b = apiBool{Present: true}
	v, err := decodeAPIValue(raw)
	if err != nil {
		return err
	}
	if a, ok := v.([]any); ok {
		if len(a) != 1 {
			return nil
		}
		v = a[0]
	}
	switch x := v.(type) {
	case nil:
		b.Valid = true
	case bool:
		b.Value = x
		b.Valid = true
	case float64:
		if x == 0 || x == 1 {
			b.Value = x == 1
			b.Valid = true
		}
	case json.Number:
		n, _ := strconv.ParseFloat(string(x), 64)
		if n == 0 || n == 1 {
			b.Value = n == 1
			b.Valid = true
		}
	case string:
		if x == "true" || x == "false" {
			b.Value = x == "true"
			b.Valid = true
		}
	}
	return nil
}

type wireNumber struct {
	Text    string
	Integer int
}

func (n apiNumber) native() (*wireNumber, error) {
	s := jsNumberString(n.Value)
	end := 0
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		end = 1
	}
	start := end
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if start == end {
		return nil, protocol.ScriptError("unsupported numeric command")
	}
	v, err := strconv.ParseInt(s[:end], 10, 32)
	if err != nil {
		return nil, protocol.ScriptError("numeric command exceeds firmware int32 range")
	}
	return &wireNumber{Text: s, Integer: int(v)}, nil
}

// Decode JSON object keys exactly. encoding/json struct matching is otherwise
// case-insensitive and can let an unknown differently-cased key override a field.
func readCompatibilityJSON(w http.ResponseWriter, r *http.Request, fields map[string]any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" && media != "text/plain" {
		compatibilityError(w, 415, "FST_ERR_CTP_INVALID_MEDIA_TYPE", "Unsupported Media Type")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		compatibilityError(w, 413, "FST_ERR_CTP_BODY_TOO_LARGE", "Request body is too large")
		return false
	}
	if media == "text/plain" {
		compatibilityError(w, 400, "FST_ERR_VALIDATION", "body must be object")
		return false
	}
	if len(raw) == 0 {
		compatibilityError(w, 400, "FST_ERR_CTP_EMPTY_JSON_BODY", "Body cannot be empty when content-type is set to 'application/json'")
		return false
	}
	if !json.Valid(raw) {
		compatibilityError(w, 400, "FST_ERR_CTP_INVALID_JSON_BODY", "Body is not valid JSON but content-type is set to 'application/json'")
		return false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		compatibilityError(w, 400, "FST_ERR_VALIDATION", "body must be object")
		return false
	}
	for name, target := range fields {
		if raw, present := object[name]; present {
			if err := json.Unmarshal(raw, target); err != nil {
				compatibilityError(w, 400, "FST_ERR_VALIDATION", "invalid JSON request")
				return false
			}
		}
	}
	return true
}

func requireNumber(w http.ResponseWriter, field string, n apiNumber) bool {
	if !n.Present {
		compatibilityError(w, 400, "FST_ERR_VALIDATION", "body must have required property '"+field+"'")
		return false
	}
	if !n.Valid {
		compatibilityError(w, 400, "FST_ERR_VALIDATION", "body/"+field+" must be number")
		return false
	}
	return true
}

func requiredField(w http.ResponseWriter, field string, present bool) bool {
	if present {
		return true
	}
	compatibilityError(w, 400, "FST_ERR_VALIDATION", "body must have required property '"+field+"'")
	return false
}

func requireString(w http.ResponseWriter, field string, value apiString) bool {
	if !value.Present {
		compatibilityError(w, 400, "FST_ERR_VALIDATION", "body must have required property '"+field+"'")
		return false
	}
	if !value.Valid {
		compatibilityError(w, 400, "FST_ERR_VALIDATION", "body/"+field+" must be string")
		return false
	}
	return true
}

func slotNumber(w http.ResponseWriter, field string, value apiNumber) (*wireNumber, bool) {
	if !numberRange(w, field, value, 0, protocol.LocalScriptSlots-1) {
		return nil, false
	}
	n, err := value.native()
	if err != nil || n.Integer < 0 || n.Integer >= protocol.LocalScriptSlots {
		compatibilityError(w, 400, "VALIDATION_ERROR", "invalid effective slot index")
		return nil, false
	}
	return n, true
}
func numberRange(w http.ResponseWriter, field string, n apiNumber, min, max float64) bool {
	if !requireNumber(w, field, n) {
		return false
	}
	if n.Value < min {
		compatibilityError(w, 400, "FST_ERR_VALIDATION", "body/"+field+" must be >= "+jsNumberString(min))
		return false
	}
	if n.Value > max {
		compatibilityError(w, 400, "FST_ERR_VALIDATION", "body/"+field+" must be <= "+jsNumberString(max))
		return false
	}
	return true
}
func requireBool(w http.ResponseWriter, field string, b apiBool) bool {
	if !b.Present {
		compatibilityError(w, 400, "FST_ERR_VALIDATION", "body must have required property '"+field+"'")
		return false
	}
	if !b.Valid {
		compatibilityError(w, 400, "FST_ERR_VALIDATION", "body/"+field+" must be boolean")
		return false
	}
	return true
}
