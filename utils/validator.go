package utils

import "errors"

// DefaultMaxLength is the default maximum input length.
const DefaultMaxLength = 255

// ValidateString checks that a string is non-empty and within the max length.
// It returns an error if validation fails.
func ValidateString(input string, maxLength int) error {
	if input == "" {
		return errors.New("input must not be empty")
	}
	if maxLength <= 0 {
		maxLength = DefaultMaxLength
	}
	if len(input) > maxLength {
		return errors.New("input exceeds maximum length")
	}
	return nil
}
