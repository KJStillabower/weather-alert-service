package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds service configuration loaded from YAML and env.
type Config struct {
	TestingMode bool

	ServerPort string

	WeatherAPIKey    string
	WeatherAPIURL    string
	WeatherAPITimeout time.Duration

	RequestTimeout time.Duration
	CacheTTL       time.Duration
	CacheBackend   string // "in_memory" or "memcached"

	MemcachedAddrs       string
	MemcachedTimeout     time.Duration
	MemcachedMaxIdleConns int

	RetryAttempts  int
	RetryBaseDelay time.Duration
	RetryMaxDelay  time.Duration
	RateLimitRPS   int
	RateLimitBurst int

	ShutdownTimeout time.Duration

	ReadyDelay            time.Duration
	OverloadWindow        time.Duration
	OverloadThresholdPct  int
	IdleThresholdReqPerMin int
	IdleWindow            time.Duration
	MinimumLifespan       time.Duration
	DegradedWindow        time.Duration
	DegradedErrorPct      int
	DegradedRetryInitial  time.Duration
	DegradedRetryMax      time.Duration

	TrackedLocations []string
}

type fileConfig struct {
	TestingMode *bool `yaml:"testing_mode"`

	Server struct {
		Port string `yaml:"port"`
	} `yaml:"server"`

	WeatherAPI struct {
		URL     string `yaml:"url"`
		Timeout string `yaml:"timeout"`
	} `yaml:"weather_api"`

	Request struct {
		Timeout string `yaml:"timeout"`
	} `yaml:"request"`

	Cache struct {
		Backend string `yaml:"backend"`
		TTL     string `yaml:"ttl"`
		Memcached struct {
			Addrs        string `yaml:"addrs"`
			Timeout      string `yaml:"timeout"`
			MaxIdleConns int    `yaml:"max_idle_conns"`
		} `yaml:"memcached"`
	} `yaml:"cache"`

	Reliability struct {
		RetryMaxAttempts int    `yaml:"retry_max_attempts"`
		RetryBaseDelay   string `yaml:"retry_base_delay"`
		RetryMaxDelay    string `yaml:"retry_max_delay"`
		RateLimitRPS     int    `yaml:"rate_limit_rps"`
		RateLimitBurst   int    `yaml:"rate_limit_burst"`
	} `yaml:"reliability"`

	Shutdown struct {
		Timeout string `yaml:"timeout"`
	} `yaml:"shutdown"`

	Lifecycle struct {
		ReadyDelay            string `yaml:"ready_delay"`
		OverloadWindow        string `yaml:"overload_window"`
		OverloadThresholdPct  int    `yaml:"overload_threshold_pct"`
		IdleThresholdReqPerMin int   `yaml:"idle_threshold_req_per_min"`
		IdleWindow            string `yaml:"idle_window"`
		MinimumLifespan       string `yaml:"minimum_lifespan"`
		DegradedWindow        string `yaml:"degraded_window"`
		DegradedErrorPct      int    `yaml:"degraded_error_pct"`
		DegradedRetryInitial  string `yaml:"degraded_retry_initial"`
		DegradedRetryMax      string `yaml:"degraded_retry_max"`
	} `yaml:"lifecycle"`

	Metrics struct {
		TrackedLocations []string `yaml:"tracked_locations"`
	} `yaml:"metrics"`
}

type secretsFile struct {
	WeatherAPIKey string `yaml:"weather_api_key"`
}

// Load reads configuration from config/{ENV_NAME}.yaml (default dev) and config/secrets.yaml.
// API key comes from WEATHER_API_KEY env or secrets file. Call from project root.
func Load() (*Config, error) {
	env := os.Getenv("ENV_NAME")
	if env == "" {
		env = "dev"
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("config: get working directory: %w", err)
	}
	configPath := filepath.Join(cwd, "config", env+".yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found: %s", configPath)
		}
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var fc fileConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	cfg := &Config{
		TestingMode: false,
	}
	if fc.TestingMode != nil {
		cfg.TestingMode = *fc.TestingMode
	}

	cfg.ServerPort = fc.Server.Port
	if cfg.ServerPort == "" {
		cfg.ServerPort = "8080"
	}

	cfg.WeatherAPIKey = os.Getenv("WEATHER_API_KEY")
	if cfg.WeatherAPIKey == "" {
		secretsPath := filepath.Join(cwd, "config", "secrets.yaml")
		secretsData, err := os.ReadFile(secretsPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("read secrets file: %w", err)
			}
		} else {
			var sec secretsFile
			if err := yaml.Unmarshal(secretsData, &sec); err != nil {
				return nil, fmt.Errorf("parse secrets file: %w", err)
			}
			cfg.WeatherAPIKey = sec.WeatherAPIKey
		}
	}
	if cfg.WeatherAPIKey == "" {
		return nil, fmt.Errorf("WEATHER_API_KEY required (set env or config/secrets.yaml weather_api_key)")
	}

	cfg.WeatherAPIURL = fc.WeatherAPI.URL
	if cfg.WeatherAPIURL == "" {
		cfg.WeatherAPIURL = "https://api.openweathermap.org/data/2.5/weather"
	}
	cfg.WeatherAPITimeout = parseDurationOrZero(fc.WeatherAPI.Timeout, 2*time.Second)

	cfg.RequestTimeout = parseDuration(fc.Request.Timeout, 5*time.Second)
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 5 * time.Second
	}
	cfg.CacheTTL = parseDuration(fc.Cache.TTL, 5*time.Minute)
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 5 * time.Minute
	}
	cfg.CacheBackend = strings.TrimSpace(strings.ToLower(os.Getenv("CACHE_BACKEND")))
	if cfg.CacheBackend == "" {
		cfg.CacheBackend = strings.TrimSpace(strings.ToLower(fc.Cache.Backend))
	}
	if cfg.CacheBackend == "" {
		cfg.CacheBackend = "in_memory"
	}
	cfg.MemcachedAddrs = strings.TrimSpace(os.Getenv("MEMCACHED_ADDRS"))
	if cfg.MemcachedAddrs == "" {
		cfg.MemcachedAddrs = strings.TrimSpace(fc.Cache.Memcached.Addrs)
	}
	if cfg.MemcachedAddrs == "" {
		cfg.MemcachedAddrs = "localhost:11211"
	}
	cfg.MemcachedTimeout = parseDuration(fc.Cache.Memcached.Timeout, 500*time.Millisecond)
	if cfg.MemcachedTimeout <= 0 {
		cfg.MemcachedTimeout = 500 * time.Millisecond
	}
	cfg.MemcachedMaxIdleConns = fc.Cache.Memcached.MaxIdleConns
	if cfg.MemcachedMaxIdleConns <= 0 {
		cfg.MemcachedMaxIdleConns = 2
	}

	cfg.RetryAttempts = fc.Reliability.RetryMaxAttempts
	if cfg.RetryAttempts <= 0 {
		cfg.RetryAttempts = 3
	}
	cfg.RetryBaseDelay = parseDuration(fc.Reliability.RetryBaseDelay, 100*time.Millisecond)
	cfg.RetryMaxDelay = parseDuration(fc.Reliability.RetryMaxDelay, 2*time.Second)
	cfg.RateLimitRPS = fc.Reliability.RateLimitRPS
	if cfg.RateLimitRPS <= 0 {
		cfg.RateLimitRPS = 100
	}
	cfg.RateLimitBurst = fc.Reliability.RateLimitBurst
	if cfg.RateLimitBurst <= 0 {
		cfg.RateLimitBurst = 250
	}

	cfg.ShutdownTimeout = parseDuration(fc.Shutdown.Timeout, 30*time.Second)

	cfg.ReadyDelay = parseDuration(fc.Lifecycle.ReadyDelay, 3*time.Second)
	cfg.OverloadWindow = parseDuration(fc.Lifecycle.OverloadWindow, 60*time.Second)
	cfg.OverloadThresholdPct = fc.Lifecycle.OverloadThresholdPct
	if cfg.OverloadThresholdPct <= 0 {
		cfg.OverloadThresholdPct = 80
	}
	cfg.IdleThresholdReqPerMin = fc.Lifecycle.IdleThresholdReqPerMin
	if cfg.IdleThresholdReqPerMin <= 0 {
		cfg.IdleThresholdReqPerMin = 5
	}
	cfg.IdleWindow = parseDuration(fc.Lifecycle.IdleWindow, 5*time.Minute)
	cfg.MinimumLifespan = parseDuration(fc.Lifecycle.MinimumLifespan, 5*time.Minute)
	cfg.DegradedWindow = parseDuration(fc.Lifecycle.DegradedWindow, 60*time.Second)
	cfg.DegradedErrorPct = fc.Lifecycle.DegradedErrorPct
	if cfg.DegradedErrorPct <= 0 {
		cfg.DegradedErrorPct = 5
	}
	cfg.DegradedRetryInitial = parseDuration(fc.Lifecycle.DegradedRetryInitial, 1*time.Minute)
	cfg.DegradedRetryMax = parseDuration(fc.Lifecycle.DegradedRetryMax, 20*time.Minute)
	cfg.TrackedLocations = fc.Metrics.TrackedLocations

	if err := validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// parseDuration parses a duration string and returns defaultVal if parsing fails or result is <= 0.
// Used for parsing duration fields from YAML config with safe fallback to defaults.
func parseDuration(s string, defaultVal time.Duration) time.Duration {
	d := parseDurationOrZero(s, defaultVal)
	if d <= 0 {
		return defaultVal
	}
	return d
}

// parseDurationOrZero parses a duration string, returning defaultVal on empty string or parse error.
// Returns zero or negative durations as-is (caller should handle fallback).
func parseDurationOrZero(s string, defaultVal time.Duration) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultVal
	}
	return d
}

// validate performs post-load validation of configuration values.
// Ensures WeatherAPITimeout is positive, RequestTimeout >= WeatherAPITimeout,
// and CacheBackend is a valid value. Auto-adjusts RequestTimeout if needed.
func validate(cfg *Config) error {
	if cfg.WeatherAPITimeout <= 0 {
		return fmt.Errorf("WEATHER_API_TIMEOUT must be positive")
	}
	if cfg.RequestTimeout <= cfg.WeatherAPITimeout {
		cfg.RequestTimeout = cfg.WeatherAPITimeout + time.Second
	}
	switch cfg.CacheBackend {
	case "in_memory", "memcached":
		// valid
	default:
		return fmt.Errorf("cache.backend must be in_memory or memcached, got %q", cfg.CacheBackend)
	}
	return nil
}
