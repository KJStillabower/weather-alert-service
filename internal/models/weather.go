package models

import "time"

type WeatherData struct {
	Location    string    `json:"location"`
	Temperature float64   `json:"temperature"`
	Conditions  string    `json:"conditions"`
	Humidity    int       `json:"humidity"`
	WindSpeed   float64   `json:"windSpeed"`
	Timestamp   time.Time `json:"timestamp"`
	Stale       bool      `json:"stale,omitempty"` // Indicates data served from stale cache
}
