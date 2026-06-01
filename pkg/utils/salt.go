package utils

import (
	"crypto/rand"
	"fmt"
)

const (
	// Recommended salt size
	saltSize = 32
)

// GenerateSalt creates a cryptographically secure random salt
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, saltSize)
	_, err := rand.Read(salt)
	if err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}
	return salt, nil
}
