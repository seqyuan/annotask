package main

import (
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadConfig loads configuration from annotask.yaml
func LoadConfig() (*Config, error) {
	// Get executable directory
	exePath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	exeDir := filepath.Dir(exePath)
	configPath := filepath.Join(exeDir, "annotask.yaml")

	config := &Config{
		Db:      filepath.Join(exeDir, "annotask.db"),
		Project: "default",
	}
	config.Retry.Max = 3
	config.Queue = "default.q"
	config.Node = []string{}
	config.SgeProject = ""
	config.Defaults.Line = 1
	config.Defaults.Thread = 1
	config.Defaults.CPU = 1

	// Try to load from file
	if _, err := os.Stat(configPath); err == nil {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %v", err)
		}
		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %v", err)
		}
	} else {
		// Create default config file
		data, err := yaml.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal default config: %v", err)
		}
		if err := os.WriteFile(configPath, data, 0644); err != nil {
			log.Printf("Warning: Could not write default config file: %v", err)
		}
	}

	// If node is empty or nil, initialize as empty slice
	// Empty node list means no node restriction for qsubsge mode
	if config.Node == nil {
		config.Node = []string{}
	}

	return config, nil
}

// GetCurrentUserID returns current user ID
func GetCurrentUserID() string {
	u, err := user.Current()
	if err != nil {
		return "unknown"
	}
	return u.Username
}

// CheckNode checks if current node is in the allowed nodes list (for qsubsge mode)
// If configNodes is empty, no restriction is applied
func CheckNode(configNodes []string) error {
	// If node list is empty, no restriction
	if len(configNodes) == 0 {
		return nil
	}

	currentNode, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to get hostname: %v", err)
	}

	// Check if current node is in the allowed list
	for _, allowedNode := range configNodes {
		if currentNode == allowedNode {
			return nil
		}
	}

	return fmt.Errorf("current node (%s) is not in allowed nodes list: %v", currentNode, configNodes)
}
