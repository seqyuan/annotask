package main

import (
	"database/sql"
	"fmt"

	"gopkg.in/yaml.v3"
)

// MySql represents a local database connection
type MySql struct {
	Db *sql.DB
}

// NodeList is a custom type that can unmarshal from both string and []string
type NodeList []string

// UnmarshalYAML implements custom YAML unmarshaling to support both string and []string
func (n *NodeList) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		*n = []string{}
		return nil
	}

	switch value.Kind {
	case yaml.ScalarNode:
		// Handle string (including empty string)
		if value.Value == "" {
			*n = []string{}
		} else {
			*n = []string{value.Value}
		}
		return nil
	case yaml.SequenceNode:
		// Handle array
		result := make([]string, 0, len(value.Content))
		for _, item := range value.Content {
			if item.Kind == yaml.ScalarNode {
				result = append(result, item.Value)
			}
		}
		*n = result
		return nil
	default:
		return fmt.Errorf("node must be a string or a list of strings, got %v", value.Kind)
	}
}

// Config represents the application configuration
type Config struct {
	Db      string `yaml:"db"`
	Project string `yaml:"project"`
	Retry   struct {
		Max int `yaml:"max"`
	} `yaml:"retry"`
	Queue      string   `yaml:"queue"`
	Node       NodeList `yaml:"node"`
	SgeProject string   `yaml:"sge_project"`
	Defaults   struct {
		Line   int `yaml:"line"`
		Thread int `yaml:"thread"`
		CPU    int `yaml:"cpu"`
	} `yaml:"defaults"`
}

// GlobalDB represents the global database connection
type GlobalDB struct {
	Db *sql.DB
}

// JobMode represents the execution mode
type JobMode string

const (
	ModeLocal   JobMode = "local"
	ModeQsubSge JobMode = "qsubsge"
)

// jobStatusType represents the status of a job
type jobStatusType string

const (
	J_pending  jobStatusType = "Pending"
	J_failed   jobStatusType = "Failed"
	J_running  jobStatusType = "Running"
	J_finished jobStatusType = "Finished"
)

// TaskStatus represents the current status of a task
type TaskStatus struct {
	subJobNum int
	status    string
	retry     int
	taskid    sql.NullString
	starttime sql.NullString
	endtime   sql.NullString
	exitCode  sql.NullInt64
}
