package validation

import (
	"errors"
	"testing"
)

func TestValidateLocation_EmptyAndWhitespace(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"spaces", "   "},
		{"tab", "\t"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateLocation(tc.input, 1, 100)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, ErrLocationEmpty) {
				t.Errorf("error = %v, want ErrLocationEmpty", err)
			}
		})
	}
}

func TestValidateLocation_TooShort(t *testing.T) {
	_, err := ValidateLocation("x", 2, 100)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrLocationTooShort) {
		t.Errorf("error = %v, want ErrLocationTooShort", err)
	}
}

func TestValidateLocation_TooLong(t *testing.T) {
	long := ""
	for i := 0; i < 101; i++ {
		long += "a"
	}
	_, err := ValidateLocation(long, 1, 100)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrLocationTooLong) {
		t.Errorf("error = %v, want ErrLocationTooLong", err)
	}
}

func TestValidateLocation_InvalidChars(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"slash", "sea/ttle"},
		{"backslash", "sea\\ttle"},
		{"question", "sea?ttle"},
		{"hash", "sea#ttle"},
		{"control", "sea\x00ttle"},
		{"percent", "sea%ttle"},
		{"ampersand", "sea&ttle"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateLocation(tc.input, 1, 100)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, ErrLocationInvalidChars) {
				t.Errorf("error = %v, want ErrLocationInvalidChars", err)
			}
		})
	}
}

func TestValidateLocation_Valid(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantNorm string
	}{
		{"simple", "Seattle", "Seattle"},
		{"with space", "New York", "New York"},
		{"comma", "London,uk", "London,uk"},
		{"hyphen", "Some-City", "Some-City"},
		{"trimmed", "  Boston  ", "Boston"},
		{"unicode", "Zürich", "Zürich"},
		{"digits", "Area51", "Area51"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateLocation(tc.input, 1, 100)
			if err != nil {
				t.Fatalf("ValidateLocation() err = %v", err)
			}
			if got != tc.wantNorm {
				t.Errorf("normalized = %q, want %q", got, tc.wantNorm)
			}
		})
	}
}

func TestValidateLocation_LengthBoundaries(t *testing.T) {
	// Exactly min length
	got, err := ValidateLocation("ab", 2, 100)
	if err != nil {
		t.Fatalf("min boundary: err = %v", err)
	}
	if got != "ab" {
		t.Errorf("min boundary: got %q", got)
	}
	// Exactly max length (100 runes)
	s100 := ""
	for i := 0; i < 100; i++ {
		s100 += "a"
	}
	got, err = ValidateLocation(s100, 1, 100)
	if err != nil {
		t.Fatalf("max boundary: err = %v", err)
	}
	if len([]rune(got)) != 100 {
		t.Errorf("max boundary: rune count = %d, want 100", len([]rune(got)))
	}
	// One over max
	s101 := s100 + "a"
	_, err = ValidateLocation(s101, 1, 100)
	if err == nil || !errors.Is(err, ErrLocationTooLong) {
		t.Errorf("over max: err = %v, want ErrLocationTooLong", err)
	}
}
