package netdef

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

var prefixes = map[byte]uint{
	'k': 1024,
	'm': 1024 * 1024,
	'g': 1024 * 1024 * 1024,
	't': 1024 * 1024 * 1024 * 1024,
}

func ParseHumanLinkRate(s string) (uint, error) {
	if s == "" {
		return 0, nil
	}

	if len(s) < 4 {
		return 0, fmt.Errorf("invalid rate limit string")
	}

	if !strings.HasSuffix(s, "bit") {
		return 0, fmt.Errorf("link rate string must end in 'bit'")
	}

	s = s[:len(s)-3]
	mul := uint(1)
	if !unicode.IsDigit(rune(s[len(s)-1])) {
		m, ok := prefixes[s[len(s)-1]]
		if !ok {
			return 0, fmt.Errorf("invalid metric prefix: %c", s[len(s)-1])
		}

		mul = m
		s = s[:len(s)-1]
	}

	val, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}

	return uint(val) * mul, nil
}

func ParsePercentage(s string) (uint, error) {
	if s == "" {
		return 0, nil
	}

	if !strings.HasSuffix(s, "%") {
		return 0, fmt.Errorf("percentage strings must end in %%")
	}

	v, err := strconv.Atoi(s[:len(s)-1])
	if err != nil {
		return 0, err
	}

	// TODO: maybe bound to [0-100]?
	return uint(v), nil
}
