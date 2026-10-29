// Package money provides exact decimal-string to int64-cents conversion.
// No floating point is used anywhere in the conversion path.
package money

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxCents is the largest absolute amount accepted for a single financial field.
// This keeps every stored amount and common arithmetic safely inside int64.
const MaxCents int64 = 999_999_999_999

var (
	ErrFormat = errors.New("金额格式不正确")
	ErrRange  = errors.New("金额超出允许范围")
)

// Parse converts a decimal amount with at most two fractional digits to cents.
// A leading minus sign is accepted (credit-card style opening balances).
// Numbers are not parsed as float64 at any point.
func Parse(s string) (int64, error) {
	if s == "" || !utf8.ValidString(s) {
		return 0, ErrFormat
	}

	negative := false
	if s[0] == '-' {
		negative = true
		s = s[1:]
		if s == "" {
			return 0, ErrFormat
		}
	}
	if s == "" || s[0] == '+' {
		return 0, ErrFormat
	}

	whole := s
	fraction := ""
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		if strings.IndexByte(s[dot+1:], '.') >= 0 {
			return 0, ErrFormat
		}
		whole = s[:dot]
		fraction = s[dot+1:]
		if whole == "" || fraction == "" || len(fraction) > 2 {
			return 0, ErrFormat
		}
	}
	if !allDigits(whole) || !allDigits(fraction) {
		return 0, ErrFormat
	}

	// Trim leading zeros before the length check so absurdly padded inputs
	// cannot be mistaken for out-of-range values.
	trimmedWhole := strings.TrimLeft(whole, "0")
	if trimmedWhole == "" {
		trimmedWhole = "0"
	}
	if len(trimmedWhole) > 12 {
		return 0, ErrRange
	}

	wholeValue, err := strconv.ParseInt(trimmedWhole, 10, 64)
	if err != nil {
		return 0, ErrRange
	}

	var fractionValue int64
	switch len(fraction) {
	case 0:
		fractionValue = 0
	case 1:
		digit, err := strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, ErrFormat
		}
		fractionValue = digit * 10
	case 2:
		digit, err := strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, ErrFormat
		}
		fractionValue = digit
	default:
		return 0, ErrFormat
	}

	if wholeValue > MaxCents/100 {
		return 0, ErrRange
	}
	cents := wholeValue*100 + fractionValue
	if cents > MaxCents {
		return 0, ErrRange
	}
	if negative {
		cents = -cents
	}
	return cents, nil
}

// Format renders cents as a decimal string with exactly two fractional digits.
func Format(cents int64) string {
	var magnitude uint64
	sign := ""
	if cents < 0 {
		sign = "-"
		// Negating math.MinInt64 would overflow, so convert through uint64.
		magnitude = uint64(-(cents + 1)) + 1
	} else {
		magnitude = uint64(cents)
	}
	return fmt.Sprintf("%s%d.%02d", sign, magnitude/100, magnitude%100)
}

func allDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
