package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad_FailsWhenNoAPIKey(t *testing.T) {
	savedKey := os.Getenv("WEATHER_API_KEY")
	os.Unsetenv("WEATHER_API_KEY")
	defer func() {
		if savedKey != "" {
			os.Setenv("WEATHER_API_KEY", savedKey)
		}
	}()

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := t.TempDir()
	writeEnvFile(t, dir, minimalEnvYAML)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	cfg, err := Load()
	if err == nil {
		t.Fatal("Load() expected error when no WEATHER_API_KEY and no secrets file, got nil")
	}
	if cfg != nil {
		t.Fatalf("Load() expected nil config on error, got %+v", cfg)
	}
	if !strings.Contains(err.Error(), "WEATHER_API_KEY") {
		t.Errorf("Load() error = %v, want message containing WEATHER_API_KEY", err)
	}
}

func TestLoad_SucceedsWithSecretsFile(t *testing.T) {
	savedKey := os.Getenv("WEATHER_API_KEY")
	os.Unsetenv("WEATHER_API_KEY")
	defer func() {
		if savedKey != "" {
			os.Setenv("WEATHER_API_KEY", savedKey)
		}
	}()

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := t.TempDir()
	writeEnvFile(t, dir, minimalEnvYAML)
	writeSecretsFile(t, dir, "weather_api_key: key-from-secrets-file\n")
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.WeatherAPIKey != "key-from-secrets-file" {
		t.Errorf("WeatherAPIKey = %q, want key from secrets file", cfg.WeatherAPIKey)
	}
}

func TestLoad_EnvFileNotFound(t *testing.T) {
	savedEnv := os.Getenv("ENV_NAME")
	os.Setenv("ENV_NAME", "nonexistent")
	defer func() {
		os.Setenv("ENV_NAME", savedEnv)
	}()

	origWd, _ := os.Getwd()
	os.Chdir(findProjectRoot(t))
	defer os.Chdir(origWd)

	cfg, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for missing env file, got nil")
	}
	if cfg != nil {
		t.Fatalf("Load() expected nil config on error, got %+v", cfg)
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "config file") {
		t.Errorf("Load() error = %v, want message about config file not found", err)
	}
}

func TestLoad_EmptyDurationFallsBackToDefault(t *testing.T) {
	savedKey := os.Getenv("WEATHER_API_KEY")
	os.Setenv("WEATHER_API_KEY", "test-key-1234567890")
	defer func() {
		os.Unsetenv("WEATHER_API_KEY")
		if savedKey != "" {
			os.Setenv("WEATHER_API_KEY", savedKey)
		}
	}()

	emptyDurationYAML := `
server:
  port: "8080"
weather_api:
  url: "https://api.example.com"
  timeout: ""
request:
  timeout: "5s"
cache:
  ttl: "5m"
reliability:
  retry_max_attempts: 3
  retry_base_delay: "100ms"
  retry_max_delay: "2s"
  rate_limit_rps: 5
  rate_limit_burst: 10
shutdown:
  timeout: "10s"
`
	origWd, _ := os.Getwd()
	dir := t.TempDir()
	writeEnvFile(t, dir, emptyDurationYAML)
	writeSecretsFile(t, dir, "weather_api_key: key\n")
	os.Chdir(dir)
	defer os.Chdir(origWd)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.WeatherAPITimeout <= 0 {
		t.Error("Load() with empty duration should fall back to default (2s for weather_api.timeout)")
	}
}

func TestLoad_InvalidDurationFallsBackToDefault(t *testing.T) {
	savedKey := os.Getenv("WEATHER_API_KEY")
	os.Setenv("WEATHER_API_KEY", "test-key-1234567890")
	defer func() {
		os.Unsetenv("WEATHER_API_KEY")
		if savedKey != "" {
			os.Setenv("WEATHER_API_KEY", savedKey)
		}
	}()

	invalidDurationYAML := `
server:
  port: "8080"
weather_api:
  url: "https://api.example.com"
  timeout: "2s"
request:
  timeout: "5s"
cache:
  ttl: "invalid"
reliability:
  retry_max_attempts: 3
  retry_base_delay: "100ms"
  retry_max_delay: "2s"
  rate_limit_rps: 5
  rate_limit_burst: 10
shutdown:
  timeout: "10s"
`
	origWd, _ := os.Getwd()
	dir := t.TempDir()
	writeEnvFile(t, dir, invalidDurationYAML)
	writeSecretsFile(t, dir, "weather_api_key: key\n")
	os.Chdir(dir)
	defer os.Chdir(origWd)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}
	if cfg.CacheTTL <= 0 {
		t.Error("Load() with invalid duration should fall back to default CacheTTL")
	}
}

func TestLoad_ValidationFailsWhenWeatherAPITimeoutZero(t *testing.T) {
	savedKey := os.Getenv("WEATHER_API_KEY")
	os.Setenv("WEATHER_API_KEY", "test-key-1234567890")
	defer func() {
		os.Unsetenv("WEATHER_API_KEY")
		if savedKey != "" {
			os.Setenv("WEATHER_API_KEY", savedKey)
		}
	}()

	zeroTimeoutYAML := `
server:
  port: "8080"
weather_api:
  url: "https://api.example.com"
  timeout: "0s"
request:
  timeout: "5s"
cache:
  ttl: "5m"
reliability:
  retry_max_attempts: 3
  retry_base_delay: "100ms"
  retry_max_delay: "2s"
  rate_limit_rps: 5
  rate_limit_burst: 10
shutdown:
  timeout: "10s"
`
	origWd, _ := os.Getwd()
	dir := t.TempDir()
	writeEnvFile(t, dir, zeroTimeoutYAML)
	writeSecretsFile(t, dir, "weather_api_key: key\n")
	os.Chdir(dir)
	defer os.Chdir(origWd)

	cfg, err := Load()
	if err == nil {
		t.Fatal("Load() expected error when WeatherAPITimeout is zero, got nil")
	}
	if cfg != nil {
		t.Fatalf("Load() expected nil config on error, got %+v", cfg)
	}
	if !strings.Contains(err.Error(), "WEATHER_API_TIMEOUT") {
		t.Errorf("Load() error = %v, want message about WEATHER_API_TIMEOUT", err)
	}
}

func TestLoad_InvalidSecretsYAML(t *testing.T) {
	savedKey := os.Getenv("WEATHER_API_KEY")
	os.Unsetenv("WEATHER_API_KEY")
	defer func() {
		if savedKey != "" {
			os.Setenv("WEATHER_API_KEY", savedKey)
		}
	}()

	origWd, _ := os.Getwd()
	dir := t.TempDir()
	writeEnvFile(t, dir, minimalEnvYAML)
	writeSecretsFile(t, dir, "not valid: yaml: [[[")
	os.Chdir(dir)
	defer os.Chdir(origWd)

	cfg, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for invalid secrets YAML, got nil")
	}
	if cfg != nil {
		t.Fatalf("Load() expected nil config on error, got %+v", cfg)
	}
	if !strings.Contains(err.Error(), "parse") && !strings.Contains(err.Error(), "secrets") {
		t.Errorf("Load() error = %v, want message about parse or secrets", err)
	}
}

func TestLoad_InvalidConfigYAML(t *testing.T) {
	savedKey := os.Getenv("WEATHER_API_KEY")
	os.Setenv("WEATHER_API_KEY", "test-key-1234567890")
	defer func() {
		os.Unsetenv("WEATHER_API_KEY")
		if savedKey != "" {
			os.Setenv("WEATHER_API_KEY", savedKey)
		}
	}()

	origWd, _ := os.Getwd()
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "dev.yaml"), []byte("not: valid: yaml: [[["), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	os.Chdir(dir)
	defer os.Chdir(origWd)

	cfg, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for invalid config YAML, got nil")
	}
	if cfg != nil {
		t.Fatalf("Load() expected nil config on error, got %+v", cfg)
	}
	if !strings.Contains(err.Error(), "parse") && !strings.Contains(err.Error(), "config") {
		t.Errorf("Load() error = %v, want message about parse or config", err)
	}
}

func TestLoad_SucceedsWithEnvVar(t *testing.T) {
	savedKey := os.Getenv("WEATHER_API_KEY")
	os.Setenv("WEATHER_API_KEY", "test-key-1234567890")
	defer func() {
		os.Unsetenv("WEATHER_API_KEY")
		if savedKey != "" {
			os.Setenv("WEATHER_API_KEY", savedKey)
		}
	}()

	origWd, _ := os.Getwd()
	os.Chdir(findProjectRoot(t))
	defer os.Chdir(origWd)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}
	if cfg.WeatherAPIKey != "test-key-1234567890" {
		t.Errorf("WeatherAPIKey = %q, want test key", cfg.WeatherAPIKey)
	}
	if cfg.WeatherAPIURL == "" || cfg.ServerPort == "" {
		t.Errorf("Load() did not populate config from config/dev.yaml")
	}
}

func TestLoad_LifecycleOverloadConfig(t *testing.T) {
	savedKey := os.Getenv("WEATHER_API_KEY")
	os.Setenv("WEATHER_API_KEY", "test-key")
	defer func() {
		if savedKey != "" {
			os.Setenv("WEATHER_API_KEY", savedKey)
		}
	}()

	lifecycleYAML := minimalEnvYAML + `
lifecycle:
  overload_window: "30s"
  overload_threshold_pct: 90
  idle_threshold_req_per_min: 3
  idle_window: "2m"
  minimum_lifespan: "1m"
  degraded_window: "60s"
  degraded_error_pct: 10
  degraded_retry_initial: "2m"
  degraded_retry_max: "15m"
`
	origWd, _ := os.Getwd()
	dir := t.TempDir()
	writeEnvFile(t, dir, lifecycleYAML)
	writeSecretsFile(t, dir, "weather_api_key: key\n")
	os.Chdir(dir)
	defer os.Chdir(origWd)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.OverloadWindow != 30*time.Second {
		t.Errorf("OverloadWindow = %v, want 30s", cfg.OverloadWindow)
	}
	if cfg.OverloadThresholdPct != 90 {
		t.Errorf("OverloadThresholdPct = %d, want 90", cfg.OverloadThresholdPct)
	}
	if cfg.IdleThresholdReqPerMin != 3 {
		t.Errorf("IdleThresholdReqPerMin = %d, want 3", cfg.IdleThresholdReqPerMin)
	}
	if cfg.IdleWindow != 2*time.Minute {
		t.Errorf("IdleWindow = %v, want 2m", cfg.IdleWindow)
	}
	if cfg.MinimumLifespan != 1*time.Minute {
		t.Errorf("MinimumLifespan = %v, want 1m", cfg.MinimumLifespan)
	}
	if cfg.DegradedWindow != 60*time.Second {
		t.Errorf("DegradedWindow = %v, want 60s", cfg.DegradedWindow)
	}
	if cfg.DegradedErrorPct != 10 {
		t.Errorf("DegradedErrorPct = %d, want 10", cfg.DegradedErrorPct)
	}
	if cfg.DegradedRetryInitial != 2*time.Minute {
		t.Errorf("DegradedRetryInitial = %v, want 2m", cfg.DegradedRetryInitial)
	}
	if cfg.DegradedRetryMax != 15*time.Minute {
		t.Errorf("DegradedRetryMax = %v, want 15m", cfg.DegradedRetryMax)
	}
}

func TestLoad_TestingModeDefaultsFalse(t *testing.T) {
	savedKey := os.Getenv("WEATHER_API_KEY")
	os.Setenv("WEATHER_API_KEY", "test-key")
	defer func() {
		if savedKey != "" {
			os.Setenv("WEATHER_API_KEY", savedKey)
		}
	}()

	origWd, _ := os.Getwd()
	dir := t.TempDir()
	writeEnvFile(t, dir, minimalEnvYAML)
	writeSecretsFile(t, dir, "weather_api_key: key\n")
	os.Chdir(dir)
	defer os.Chdir(origWd)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.TestingMode {
		t.Error("TestingMode = true, want false when omitted (default)")
	}
}

func TestLoad_TestingModeTrue(t *testing.T) {
	savedKey := os.Getenv("WEATHER_API_KEY")
	os.Setenv("WEATHER_API_KEY", "test-key")
	defer func() {
		if savedKey != "" {
			os.Setenv("WEATHER_API_KEY", savedKey)
		}
	}()

	yamlWithTesting := minimalEnvYAML + "\ntesting_mode: true\n"
	origWd, _ := os.Getwd()
	dir := t.TempDir()
	writeEnvFile(t, dir, yamlWithTesting)
	writeSecretsFile(t, dir, "weather_api_key: key\n")
	os.Chdir(dir)
	defer os.Chdir(origWd)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.TestingMode {
		t.Error("TestingMode = false, want true")
	}
}

const minimalEnvYAML = `
server:
  port: "8080"
weather_api:
  url: "https://api.example.com"
  timeout: "2s"
request:
  timeout: "5s"
cache:
  ttl: "5m"
reliability:
  retry_max_attempts: 3
  retry_base_delay: "100ms"
  retry_max_delay: "2s"
  rate_limit_rps: 5
  rate_limit_burst: 10
shutdown:
  timeout: "10s"
`

func writeEnvFile(t *testing.T, dir, content string) {
	t.Helper()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "dev.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
}

func writeSecretsFile(t *testing.T, dir, content string) {
	t.Helper()
	secretsDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(secretsDir, 0755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "secrets.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("write secrets file: %v", err)
	}
}

// TestCoverageGaps_IntentionallyUntested documents paths we reviewed but chose not to test.
// Run with -v to see skip reasons. These gaps do not affect coverage targets.
func TestCoverageGaps_IntentionallyUntested(t *testing.T) {
	t.Run("loadAPIKeyFromSecrets_read_error", func(t *testing.T) {
		t.Skip("read-error path (non-IsNotExist) requires simulated ReadFile failure; would need OS-specific tricks or afero, not worth portability cost")
	})
	t.Run("Load_read_config_error", func(t *testing.T) {
		t.Skip("ReadFile error path (permission denied, etc.) same as loadAPIKeyFromSecrets; would require injecting failure")
	})
	t.Run("validate_RequestTimeout_branch", func(t *testing.T) {
		t.Skip("RequestTimeout <= WeatherAPITimeout is dead code: Load() auto-adjusts before validate(); unreachable without refactor")
	})
}

func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "config", "dev.yaml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("config/dev.yaml not found (run tests from project root)")
		}
		dir = parent
	}
}
