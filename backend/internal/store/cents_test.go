package store

import "testing"

func TestParseAggregateCents(t *testing.T) {
	valid := map[string]int64{
		"0":                       0,
		"-0":                      0,
		"120":                     120,
		"-750":                    -750,
		"123.00":                  123,
		"9223372036854775807":     9223372036854775807,
		"-9223372036854775808":    -9223372036854775808,
	}
	for input, want := range valid {
		got, err := parseAggregateCents(input)
		if err != nil {
			t.Fatalf("parseAggregateCents(%q) 意外错误: %v", input, err)
		}
		if got != want {
			t.Fatalf("parseAggregateCents(%q) = %d, want %d", input, got, want)
		}
	}

	invalid := []string{
		"",
		"+1",
		"1.50",
		"-1.5",
		"9223372036854775808",
		"-9223372036854775809",
		"1e12",
		"NaN",
	}
	for _, input := range invalid {
		if _, err := parseAggregateCents(input); err == nil {
			t.Fatalf("parseAggregateCents(%q) 应返回错误", input)
		}
	}
}
