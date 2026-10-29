package store

import (
	"errors"
	"strconv"
	"strings"
)

// parseAggregateCents converts PostgreSQL numeric::text aggregates of cents to
// int64 with an explicit overflow check. PostgreSQL SUM(bigint) yields numeric,
// so this is the only place where aggregate results become native integers.
func parseAggregateCents(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, errors.New("金额聚合结果为空")
	}

	negative := false
	if s[0] == '-' {
		negative = true
		s = s[1:]
	}
	if s == "" || s[0] == '+' {
		return 0, errors.New("金额聚合结果格式无效")
	}

	whole := s
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		if strings.IndexByte(s[dot+1:], '.') >= 0 {
			return 0, errors.New("金额聚合结果格式无效")
		}
		whole = s[:dot]
		fraction := s[dot+1:]
		if whole == "" || fraction == "" {
			return 0, errors.New("金额聚合结果格式无效")
		}
		for i := range len(fraction) {
			if fraction[i] != '0' {
				return 0, errors.New("金额聚合结果包含非整数分")
			}
		}
	}
	if whole == "" {
		return 0, errors.New("金额聚合结果格式无效")
	}
	for i := range len(whole) {
		if whole[i] < '0' || whole[i] > '9' {
			return 0, errors.New("金额聚合结果格式无效")
		}
	}

	if negative {
		whole = "-" + whole
	}
	value, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, errors.New("金额聚合结果超出可表示范围")
	}
	return value, nil
}
