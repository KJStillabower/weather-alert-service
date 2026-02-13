package client

import (
	"context"
	"errors"
	"strings"
)

// ErrorCategory is a stable label for error classification in metrics.
type ErrorCategory string

// Error category constants used as metric labels (weatherApiErrorsTotal, httpErrorsTotal).
const (
	ErrorCategoryTimeout          ErrorCategory = "timeout"
	ErrorCategoryNetwork          ErrorCategory = "network"
	ErrorCategoryInvalidAPIKey    ErrorCategory = "invalid_api_key"
	ErrorCategoryLocationNotFound ErrorCategory = "location_not_found"
	ErrorCategoryRateLimited      ErrorCategory = "rate_limited"
	ErrorCategoryUpstream5xx      ErrorCategory = "upstream_5xx"
	ErrorCategoryParsing          ErrorCategory = "parsing"
	ErrorCategoryValidation       ErrorCategory = "validation"
	ErrorCategoryCache            ErrorCategory = "cache"
	ErrorCategoryUnknown          ErrorCategory = "unknown"
)

// CategorizeError maps an error to a stable ErrorCategory for metrics.
func CategorizeError(err error) ErrorCategory {
	if err == nil {
		return ""
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrorCategoryTimeout
	}

	errStr := err.Error()
	if strings.Contains(errStr, "network") || strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "connection refused") {
		return ErrorCategoryNetwork
	}

	if errors.Is(err, ErrInvalidAPIKey) {
		return ErrorCategoryInvalidAPIKey
	}

	if errors.Is(err, ErrLocationNotFound) {
		return ErrorCategoryLocationNotFound
	}

	if errors.Is(err, ErrRateLimited) {
		return ErrorCategoryRateLimited
	}

	if errors.Is(err, ErrUpstreamFailure) {
		return ErrorCategoryUpstream5xx
	}

	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "context deadline exceeded") {
		return ErrorCategoryTimeout
	}

	if strings.Contains(errStr, "parse") || strings.Contains(errStr, "unmarshal") {
		return ErrorCategoryParsing
	}

	if strings.Contains(errStr, "invalid") || strings.Contains(errStr, "validation") {
		return ErrorCategoryValidation
	}

	if strings.Contains(errStr, "cache") {
		return ErrorCategoryCache
	}

	return ErrorCategoryUnknown
}
