package models

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
)

// UnmarshalJSON decodes ScanResultData with json.Number for its `any`
// fields (QueryID, RecommendedVersion), so 64-bit SAST query IDs above 2^53
// are kept exactly. A numeric wire value is therefore a json.Number (it was a
// float64 before); string values stay strings.
func (d *ScanResultData) UnmarshalJSON(b []byte) error {
	type plain ScanResultData
	var v plain
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return err
	}
	*d = ScanResultData(v)
	return nil
}

// QueryIDString returns the query ID as exact decimal digits. ok is false
// when the ID is missing, not an integer, or a float64 that cannot represent
// it exactly (above 2^53).
func (d ScanResultData) QueryIDString() (string, bool) {
	switch x := d.QueryID.(type) {
	case json.Number:
		return integerDigits(string(x))
	case string:
		return integerDigits(x)
	case float64:
		if x != math.Trunc(x) || math.Abs(x) >= 1<<53 {
			return "", false
		}
		return strconv.FormatFloat(x, 'f', 0, 64), true
	case int:
		return strconv.Itoa(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case uint64:
		return strconv.FormatUint(x, 10), true
	default:
		return "", false
	}
}

func integerDigits(s string) (string, bool) {
	if s == "" {
		return "", false
	}
	start := 0
	if s[0] == '-' {
		start = 1
	}
	if start == len(s) {
		return "", false
	}
	for i := start; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return "", false
		}
	}
	return s, true
}
