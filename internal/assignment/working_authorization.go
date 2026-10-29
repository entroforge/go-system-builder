package assignment

import "github.com/entroforge/go-system-builder/internal/agentauth"

func ValidateWorkingAuthorization(root string, state map[string]any, owner string) error {
	return agentauth.ValidateWorkingAuthorization(root, state, owner)
}
