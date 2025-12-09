package main

import "database/sql"

// MySql represents a local database connection
type MySql struct {
	Db *sql.DB
}

// Config represents the application configuration
type Config struct {
	Db      string `yaml:"db"`
	Project string `yaml:"project"`
	Retry   struct {
		Max int `yaml:"max"`
	} `yaml:"retry"`
	Queue      string   `yaml:"queue"`
	Node       []string `yaml:"node"`
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
