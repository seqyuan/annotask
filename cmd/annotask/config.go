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
// User home config (~/.annotask/annotask.yaml) takes precedence over executable directory config
// If user config's db path doesn't exist, fall back to system config's db path
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
	userConfigDir := filepath.Join(usr.HomeDir, ".annotask")
	userConfigPath := filepath.Join(userConfigDir, "annotask.yaml")
	
	// Default database path: use user home directory to avoid permission issues
	// If executable is in system directory (e.g., /usr/bin), user may not have write permission
	defaultDbPath := filepath.Join(usr.HomeDir, ".annotask", "annotask.db")

	// Initialize with defaults
	config := &Config{
		Db:      defaultDbPath,
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
	var systemDbPath string
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
		// Save system db path before merging
		if exeConfig.Db != "" {
			systemDbPath = exeConfig.Db
		}
		// Merge executable config (only non-empty values)
		mergeConfig(config, &exeConfig)
	} else {
		// Config file doesn't exist, create a default one
		// Create a default config for executable directory
		// Note: Db path should use user home directory to avoid permission issues
		defaultExeConfig := &Config{
			Db:      defaultDbPath,
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
		systemDbPath = defaultDbPath
	}

	// Then, load from user home config (if exists) - this takes precedence for non-db settings
	// BUT: For global database path, we ALWAYS use executable directory config (not user home)
	// This ensures all annotask instances use the same global database
	if _, err := os.Stat(userConfigPath); err == nil {
		data, err := os.ReadFile(userConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read user config file: %v", err)
		}
		var userConfig Config
		if err := yaml.Unmarshal(data, &userConfig); err != nil {
			return nil, fmt.Errorf("failed to parse user config file: %v", err)
		}
		// Temporarily save current Db value (from system config)
		currentDbPath := config.Db
		// Merge user config (takes precedence for non-db settings)
		mergeConfig(config, &userConfig)
		// Restore system db path (don't use user's db path for global database)
		config.Db = currentDbPath
	}

	// For global database, ALWAYS use executable directory config's db path (not user home)
	// If system db path is configured, use it; otherwise use default (which will be created)
	if systemDbPath != "" {
		// Use system db path from executable directory config
		config.Db = systemDbPath
	} else {
		// No system db path configured, use default (will be created)
		config.Db = defaultDbPath
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
	// Db is NOT merged here - it should always use executable directory config
	// to ensure all annotask instances use the same global database
}

// EnsureUserConfig creates user home config file if it doesn't exist
func EnsureUserConfig() error {
	usr, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user: %v", err)
	}
	userConfigDir := filepath.Join(usr.HomeDir, ".annotask")
	userConfigPath := filepath.Join(userConfigDir, "annotask.yaml")

	// Check if file exists
	if _, err := os.Stat(userConfigPath); err == nil {
		// File exists, do nothing
		return nil
	}

	// Create .annotask directory if it doesn't exist
	if err := os.MkdirAll(userConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create user config directory: %v", err)
	}

	// Create default user config with essential fields
	// User config should contain: db, retry.max, queue, sge_project
	defaultDbPath := filepath.Join(usr.HomeDir, ".annotask", "annotask.db")
	type UserConfigStruct struct {
		Db         string `yaml:"db"`
		Retry      struct {
			Max int `yaml:"max"`
		} `yaml:"retry"`
		Queue      string `yaml:"queue"`
		SgeProject string `yaml:"sge_project"`
	}
	
	userConfig := UserConfigStruct{}
	userConfig.Db = defaultDbPath
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
// If configNodes is not empty, current node must be in the list, otherwise returns error
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

	// Current node is not in the allowed list
	return fmt.Errorf("current node (%s) is not in allowed nodes list: %v. Please run annotask qsubsge on one of the allowed nodes", currentNode, configNodes)
}
