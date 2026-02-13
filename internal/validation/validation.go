package validation

import (
	"errors"
	"strings"
	"unicode"
)

// ErrLocationEmpty is returned when location is empty or whitespace-only after trim.
var ErrLocationEmpty = errors.New("location is required")

// ErrLocationTooShort is returned when location length is below the minimum.
var ErrLocationTooShort = errors.New("location too short")

// ErrLocationTooLong is returned when location length exceeds the maximum.
var ErrLocationTooLong = errors.New("location too long")

// ErrLocationInvalidChars is returned when location contains disallowed characters.
var ErrLocationInvalidChars = errors.New("location contains invalid characters")

// ValidateLocation trims the input, enforces length bounds (minLen, maxLen in runes),
// and restricts to allowed characters: letters (Unicode), digits, space, comma, hyphen.
// Returns the trimmed string or an error suitable for 400 INVALID_LOCATION responses.
// Normalization (e.g. lowercase) is left to the service layer.
func ValidateLocation(input string, minLen, maxLen int) (string, error) {
	s := strings.TrimSpace(input)
	r := []rune(s)
	n := len(r)
	if n == 0 {
		return "", ErrLocationEmpty
	}
	if minLen > 0 && n < minLen {
		return "", ErrLocationTooShort
	}
	if maxLen > 0 && n > maxLen {
		return "", ErrLocationTooLong
	}
	for _, c := range r {
		if !isAllowedLocationRune(c) {
			return "", ErrLocationInvalidChars
		}
	}
	return s, nil
}

// isAllowedLocationRune returns true for letters (Unicode), digits, space, comma, hyphen.
func isAllowedLocationRune(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsNumber(r) {
		return true
	}
	switch r {
	case ' ', ',', '-':
		return true
	}
	return false
}
