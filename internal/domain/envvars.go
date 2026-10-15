package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

// EncodeEnvVars serializes env vars to the JSON stored in workspaces.env_vars.
// A nil slice is stored as "[]" so the column is always a JSON array.
func EncodeEnvVars(vars []EnvVars) (string, error) {
	if vars == nil {
		vars = []EnvVars{}
	}
	b, err := json.Marshal(vars)
	if err != nil {
		return "", fmt.Errorf("encode env vars: %w", err)
	}
	return string(b), nil
}

// DecodeEnvVars parses the JSON stored in workspaces.env_vars.
// An empty value decodes to an empty, non-nil slice.
func DecodeEnvVars(raw string) ([]EnvVars, error) {
	if strings.TrimSpace(raw) == "" || raw == "null" {
		return []EnvVars{}, nil
	}
	var vars []EnvVars
	if err := json.Unmarshal([]byte(raw), &vars); err != nil {
		return nil, fmt.Errorf("decode env vars: %w", err)
	}
	if vars == nil {
		vars = []EnvVars{}
	}
	return vars, nil
}

// ValidateEnvVars checks that every env var has a name and that names are unique.
func ValidateEnvVars(vars []EnvVars) error {
	seen := make(map[string]struct{}, len(vars))
	for i, v := range vars {
		if strings.TrimSpace(v.Name) == "" {
			return fmt.Errorf("envVars[%d].name is empty: %w", i, ErrInvalid)
		}
		if _, dup := seen[v.Name]; dup {
			return fmt.Errorf("envVars[%d].name %q is duplicated: %w", i, v.Name, ErrInvalid)
		}
		seen[v.Name] = struct{}{}
	}
	return nil
}
