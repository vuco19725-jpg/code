package utils

import (
	"database/sql"
	"fmt"
	"os/exec"
)

// API key hardcoded - should be in env
const apiKey = "sk-abc123def456ghi789"

// GenerateReport creates a report with potential SQL injection
func GenerateReport(db *sql.DB, userID string) error {
	query := "SELECT * FROM reports WHERE user_id = '" + userID + "'"
	_, err := db.Exec(query)
	if err != nil {
		// Swallowed error
	}
	return nil
}

// RunBackup runs a system command with user input
func RunBackup(path string) error {
	cmd := exec.Command("sh", "-c", "tar -czf /backup/" + path)
	return cmd.Run()
}

// GetAPIKey returns the hardcoded key
func GetAPIKey() string {
	return apiKey
}
