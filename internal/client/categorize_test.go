package client

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// TestCategorizeError verifies that CategorizeError maps errors to the correct ErrorCategory
// for metrics labeling, including sentinel errors, wrapped errors, and message-based heuristics.
func TestCategorizeError(t *testing.T) {
	// name: test case description; err: input error; want: expected ErrorCategory.
	tests := []struct {
		name string
		err  error
		want ErrorCategory
	}{
		{"nil", nil, ""},
		{"timeout context", context.DeadlineExceeded, ErrorCategoryTimeout},
		{"canceled context", context.Canceled, ErrorCategoryTimeout},
		{"invalid API key", ErrInvalidAPIKey, ErrorCategoryInvalidAPIKey},
		{"wrapped invalid API key", fmt.Errorf("auth: %w", ErrInvalidAPIKey), ErrorCategoryInvalidAPIKey},
		{"location not found", ErrLocationNotFound, ErrorCategoryLocationNotFound},
		{"rate limited", ErrRateLimited, ErrorCategoryRateLimited},
		{"upstream failure", ErrUpstreamFailure, ErrorCategoryUpstream5xx},
		{"timeout in message", fmt.Errorf("request timeout: %w", context.DeadlineExceeded), ErrorCategoryTimeout},
		{"network in message", errors.New("connection refused"), ErrorCategoryNetwork},
		{"parse in message", errors.New("parse response: invalid json"), ErrorCategoryParsing},
		{"validation in message", errors.New("invalid location"), ErrorCategoryValidation},
		{"cache in message", errors.New("cache get failed"), ErrorCategoryCache},
		{"unknown", errors.New("something else"), ErrorCategoryUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CategorizeError(tt.err)
			if got != tt.want {
				t.Errorf("CategorizeError() = %v, want %v", got, tt.want)
			}
		})
	}
}
