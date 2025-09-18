package util

import "strings"

// GetEnv returns environment variable value or default
func GetEnv(key, defaultValue string) string {
	// Mock implementation
	return defaultValue
}

// WriteFile writes data to file
func WriteFile(filename string, data []byte) error {
	// Mock implementation
	return nil
}

// ParseStringSlice parses comma-separated string into slice
func ParseStringSlice(value string) []string {
	if value == "" {
		return []string{}
	}
	return strings.Split(value, ",")
}