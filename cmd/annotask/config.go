package main

import (
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadConfig loads configuration from user home directory and executable directory
// User home config (~/.annotask.yml) takes precedence over executable directory config
func LoadConfig() (*Config, error) {
	// Get executable directory
	exePath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	exeDir := filepath.Dir(exePath)
	exeConfigPath := filepath.Join(exeDir, "annotask.yaml")

	// Get user home directory
	usr, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %v", err)
	}
	userConfigPath := filepath.Join(usr.HomeDir, ".annotask.yml")

	// Initialize with defaults
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
	config.MonitorUpdateInterval = 60 // Default: update every 60 seconds (1 minute)

	// First, load from executable directory config (if exists)
	// If it doesn't exist, create a default one
	if _, err := os.Stat(exeConfigPath); err == nil {
		// Config file exists, load it
		data, err := os.ReadFile(exeConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read executable config file: %v", err)
		}
		var exeConfig Config
		if err := yaml.Unmarshal(data, &exeConfig); err != nil {
			return nil, fmt.Errorf("failed to parse executable config file: %v", err)
		}
		// Merge executable config (only non-empty values)
		mergeConfig(config, &exeConfig)
	} else {
		// Config file doesn't exist, create a default one
		// Create a default config for executable directory
		defaultExeConfig := &Config{
			Db:      filepath.Join(exeDir, "annotask.db"),
			Project: "default",
		}
		defaultExeConfig.Retry.Max = 3
		defaultExeConfig.Queue = "default.q"
		defaultExeConfig.Node = []string{}
		defaultExeConfig.SgeProject = ""
		defaultExeConfig.Defaults.Line = 1
		defaultExeConfig.Defaults.Thread = 1
		defaultExeConfig.Defaults.CPU = 1
		defaultExeConfig.MonitorUpdateInterval = 60

		data, err := yaml.Marshal(defaultExeConfig)
		if err != nil {
			log.Printf("Warning: Could not marshal default executable config: %v", err)
		} else {
			if err := os.WriteFile(exeConfigPath, data, 0644); err != nil {
				log.Printf("Warning: Could not write default executable config file: %v", err)
			} else {
				log.Printf("Created default config file: %s", exeConfigPath)
			}
		}
	}

	// Then, load from user home config (if exists) - this takes precedence
	if _, err := os.Stat(userConfigPath); err == nil {
		data, err := os.ReadFile(userConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read user config file: %v", err)
		}
		var userConfig Config
		if err := yaml.Unmarshal(data, &userConfig); err != nil {
			return nil, fmt.Errorf("failed to parse user config file: %v", err)
		}
		// Merge user config (takes precedence)
		mergeConfig(config, &userConfig)
	}

	// If node is empty or nil, initialize as empty slice
	// Empty node list means no node restriction for qsubsge mode
	if len(config.Node) == 0 {
		config.Node = []string{}
	}

	return config, nil
}

// mergeConfig merges source config into target config
// Only non-empty/non-zero values from source are merged
func mergeConfig(target, source *Config) {
	if source.Project != "" {
		target.Project = source.Project
	}
	if source.Retry.Max > 0 {
		target.Retry.Max = source.Retry.Max
	}
	if source.Queue != "" {
		target.Queue = source.Queue
	}
	if len(source.Node) > 0 {
		target.Node = source.Node
	}
	if source.SgeProject != "" {
		target.SgeProject = source.SgeProject
	}
	if source.Defaults.Line > 0 {
		target.Defaults.Line = source.Defaults.Line
	}
	if source.Defaults.Thread > 0 {
		target.Defaults.Thread = source.Defaults.Thread
	}
	if source.Defaults.CPU > 0 {
		target.Defaults.CPU = source.Defaults.CPU
	}
	if source.MonitorUpdateInterval > 0 {
		target.MonitorUpdateInterval = source.MonitorUpdateInterval
	}
	// Db is always from executable directory, don't merge
}

// EnsureUserConfig creates user home config file if it doesn't exist
func EnsureUserConfig() error {
	usr, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user: %v", err)
	}
	userConfigPath := filepath.Join(usr.HomeDir, ".annotask.yml")

	// Check if file exists
	if _, err := os.Stat(userConfigPath); err == nil {
		// File exists, do nothing
		return nil
	}

	// Create default user config
	userConfig := &Config{
		Project: "default",
	}
	userConfig.Retry.Max = 3
	userConfig.Queue = "sci.q"
	userConfig.SgeProject = ""

	data, err := yaml.Marshal(userConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal user config: %v", err)
	}

	if err := os.WriteFile(userConfigPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write user config file: %v", err)
	}

	log.Printf("Created user config file: %s", userConfigPath)
	return nil
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
