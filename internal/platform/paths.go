package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

func DefaultDatabasePath() (string, error) {
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user data directory: %w", err)
	}
	return filepath.Join(configDirectory, "Noema", "noema.db"), nil
}

func EnsureParentDirectory(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	return nil
}
