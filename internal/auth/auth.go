package auth

import (
	"fmt"
	"strings"
)

type Role string

const (
	RoleRider Role = "rider"
	RoleDriver Role = "driver"
	RoleAdmin Role = "admin"
)

func RequireRole(role Role, token string) error {
	if role == "" {
		return fmt.Errorf("missing role")
	}
	if token == "" {
		return fmt.Errorf("missing token")
	}

	received := strings.TrimSpace(token)
	received = strings.TrimPrefix(received, "Bearer ")
	if received == "" {
		return fmt.Errorf("missing token")
	}
	if received == string(role) {
		return nil
	}
	return fmt.Errorf("role mismatch: required %s, got %s", role, received)
}
