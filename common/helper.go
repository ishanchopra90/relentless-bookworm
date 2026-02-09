package common

import (
	"os"
	"strconv"
	"time"
)

// GetEnv returns the environment variable value or a fallback if unset.
func GetEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

// ParseDuration parses a duration string with a fallback.
func ParseDuration(value string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// ParseInt parses an int string with a fallback.
func ParseInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
