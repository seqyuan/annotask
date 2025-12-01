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
	config.Node = ""
	config.Defaults.Line = 1
	config.Defaults.Thread = 1
	config.Defaults.CPU = 1
	config.Defaults.Mem = 1

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

	// If node is empty, get current hostname
	if config.Node == "" {
		hostname, err := os.Hostname()
		if err != nil {
			log.Printf("Warning: Could not get hostname: %v", err)
			config.Node = "unknown"
		} else {
			config.Node = hostname
		}
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

// CheckNode checks if current node matches config node (for qsubsge mode)
func CheckNode(configNode string) error {
	currentNode, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to get hostname: %v", err)
	}
	if currentNode != configNode {
		return fmt.Errorf("current node (%s) does not match config node (%s)", currentNode, configNode)
	}
	return nil
}

