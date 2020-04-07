package config

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// ParseBytes returns the number of bytes express using human notation like 1MB
// or 1.5GB.
func ParseBytes(s string) (uint64, error) {
	lastDigit := 0
	hasComma := false
	for _, r := range s {
		if !(unicode.IsDigit(r) || r == '.' || r == ',') {
			break
		}
		if r == ',' {
			hasComma = true
		}
		lastDigit++
	}

	num := s[:lastDigit]
	if hasComma {
		num = strings.Replace(num, ",", "", -1)
	}

	if num == "" {
		return 0, errors.New("invalid number")
	}

	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, err
	}

	unit := strings.ToLower(strings.TrimSpace(s[lastDigit:]))
	switch unit {
	case "b", "":
		// Do nothing
	case "k", "kb":
		f *= 1 << 10
	case "m", "mb":
		f *= 1 << 20
	case "g", "gb":
		f *= 1 << 30
	case "t", "tb":
		f *= 1 << 40
	case "p", "pb":
		f *= 1 << 50
	case "e", "eb":
		f *= 1 << 60
	default:
		return 0, fmt.Errorf("unknown unit name: %v", unit)
	}
	if f >= math.MaxUint64 {
		return 0, fmt.Errorf("too large: %v", s)
	}
	return uint64(f), nil
}
