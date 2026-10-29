package money

import (
	"errors"
	"testing"
)

func TestParseValid(t *testing.T) {
	cases := []struct {
		input string
		want  int64
	}{
		{"0", 0},
		{"0.00", 0},
		{"-0.00", 0},
		{"1", 100},
		{"1.2", 120},
		{"1.20", 120},
		{"0001.05", 105},
		{"-1.23", -123},
		{"-0.01", -1},
		{"9999999999.99", MaxCents},
		{"-9999999999.99", -MaxCents},
	}
	for _, tc := range cases {
		got, err := Parse(tc.input)
		if err != nil {
			t.Fatalf("Parse(%q) unexpected error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("Parse(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestParseInvalidFormat(t *testing.T) {
	cases := []string{
		"", " ", "+1.00", ".50", "1.", "1.234", "1,000", "1e3",
		"abc", "--1", "1 00", "１２.00", "NaN", "Infinity",
	}
	for _, input := range cases {
		if _, err := Parse(input); !errors.Is(err, ErrFormat) {
			t.Fatalf("Parse(%q) error = %v, want ErrFormat", input, err)
		}
	}
}

func TestParseBounds(t *testing.T) {
	cases := []string{
		"10000000000.00",
		"-10000000000.00",
		"999999999999999999999999999999.00",
	}
	for _, input := range cases {
		if _, err := Parse(input); !errors.Is(err, ErrRange) {
			t.Fatalf("Parse(%q) error = %v, want ErrRange", input, err)
		}
	}
}

func TestFormat(t *testing.T) {
	cases := []struct {
		input int64
		want  string
	}{
		{0, "0.00"},
		{1, "0.01"},
		{120, "1.20"},
		{-1, "-0.01"},
		{-123, "-1.23"},
		{MaxCents, "9999999999.99"},
	}
	for _, tc := range cases {
		if got := Format(tc.input); got != tc.want {
			t.Fatalf("Format(%d) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
