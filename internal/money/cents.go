// Package money represents monetary amounts as integer cents.
package money

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ErrInvalidAmount means the value cannot be used as money in this API
// (bad parse, non-positive input, too many fractional digits, or an unformattable value).
var ErrInvalidAmount = errors.New("invalid amount")

// Cents is a monetary amount in minor units (100 = 1.00).
// After ApplySign it may be negative. JSON request bodies are positive only;
// responses write a decimal number, never a float64.
type Cents int64

// ParsePositiveDecimal converts a base-10 magnitude such as "123.45" into cents.
// The value must be greater than zero and have at most two fractional digits.
func ParsePositiveDecimal(raw string) (Cents, error) {
	if raw == "" || strings.ContainsAny(raw, "eE+- \t") {
		return 0, ErrInvalidAmount
	}
	if strings.Count(raw, ".") > 1 {
		return 0, ErrInvalidAmount
	}

	whole, frac, hasFrac := strings.Cut(raw, ".")
	if !hasFrac {
		frac = "00"
	} else {
		if frac == "" || len(frac) > 2 || !digitsOnly(frac) {
			return 0, ErrInvalidAmount
		}
		if len(frac) == 1 {
			frac += "0"
		}
	}
	if whole == "" || !digitsOnly(whole) {
		return 0, ErrInvalidAmount
	}

	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, ErrInvalidAmount
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil || f < 0 || f > 99 {
		return 0, ErrInvalidAmount
	}
	if w > (math.MaxInt64-f)/100 {
		return 0, ErrInvalidAmount
	}
	cents := w*100 + f
	if cents <= 0 {
		return 0, ErrInvalidAmount
	}
	return Cents(cents), nil
}

// Format renders cents as a decimal with two fractional digits.
func (c Cents) Format() (string, error) {
	v := int64(c)
	if v == math.MinInt64 {
		return "", ErrInvalidAmount
	}
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100), nil
}

// MarshalJSON writes a JSON number. It does not use float64.
func (c Cents) MarshalJSON() ([]byte, error) {
	s, err := c.Format()
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

// UnmarshalJSON accepts a positive JSON number with at most two fractional digits.
func (c *Cents) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] == '"' {
		return ErrInvalidAmount
	}
	parsed, err := ParsePositiveDecimal(string(b))
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}

func digitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
