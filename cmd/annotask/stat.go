package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/akamensky/argparse"
)

// RunStatCommand runs the stat subcommand
func RunStatCommand(globalDB *GlobalDB, projectFilter string, showShellPath bool) error {
	usrID := GetCurrentUserID()

	var rows *sql.Rows
	var err error

	if projectFilter != "" {
		rows, err = globalDB.Db.Query(`
			SELECT status, pendingTasks, failedTasks, runningTasks
			FROM tasks
			WHERE usrID=? AND project=?
			ORDER BY project, starttime DESC
		`, usrID, projectFilter)
	} else {
		rows, err = globalDB.Db.Query(`
			SELECT status, pendingTasks, failedTasks, runningTasks
			FROM tasks
			WHERE usrID=?
			ORDER BY project, starttime DESC
		`, usrID)
	}

	if err != nil {
		return fmt.Errorf("failed to query tasks: %v", err)
	}
	defer rows.Close()

	if showShellPath {
		// Only output shell paths when -m is used
		// Note: -m flag requires status query, but we need shellPath, so query separately
		if projectFilter != "" {
			rows, err = globalDB.Db.Query(`
				SELECT shellPath
				FROM tasks
				WHERE usrID=? AND project=?
				ORDER BY project, starttime DESC
			`, usrID, projectFilter)
		} else {
			rows, err = globalDB.Db.Query(`
				SELECT shellPath
				FROM tasks
				WHERE usrID=?
				ORDER BY project, starttime DESC
			`, usrID)
		}
		if err != nil {
			return fmt.Errorf("failed to query tasks: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			var shellPath string
			err := rows.Scan(&shellPath)
			if err != nil {
				log.Printf("Error scanning row: %v", err)
				continue
			}
			fmt.Println(shellPath)
			count++
		}
		// fmt.Printf("Total records: %d\n", count)
	} else {
		// Simplified output: only status and num (Pending/Failed/Running)
		var count int
		for rows.Next() {
			var status sql.NullString
			var pending, failed, running int

			err := rows.Scan(&status, &pending, &failed, &running)
			if err != nil {
				log.Printf("Error scanning row: %v", err)
				continue
			}

			// Format: status num (Pending/Failed/Running)
			statusStr := "-"
			if status.Valid {
				statusStr = status.String
			}
			num := fmt.Sprintf("%d/%d/%d", pending, failed, running)
			fmt.Printf("%s\t%s\n", statusStr, num)
			count++
		}
	}

	return nil
}

// RunStatModule runs the stat module
func RunStatModule(config *Config, args []string) {
	// Initialize global DB
	globalDB, err := InitGlobalDB(config.Db)
	if err != nil {
		log.Fatalf("Failed to initialize global DB: %v", err)
	}
	defer globalDB.Db.Close()

	// Parse stat command arguments
	statParser := argparse.NewParser("annotask stat", "Query task status from global database")
	opt_project := statParser.String("p", "project", &argparse.Options{Help: "Filter by project name"})
	opt_module := statParser.Flag("m", "module", &argparse.Options{Help: "Show shell path (requires -p)"})

	// Prepend program name for argparse.Parse (it expects os.Args-like format)
	parseArgs := append([]string{"annotask"}, args...)
	err = statParser.Parse(parseArgs)
	if err != nil {
		// If help is requested, show module help
		errStr := err.Error()
		if strings.Contains(strings.ToLower(errStr), "help") {
			printModuleHelp("stat", config)
			return
		}
		fmt.Print(statParser.Usage(err))
		os.Exit(1)
	}

	// Check if -m is used without -p
	if opt_module != nil && *opt_module {
		if opt_project == nil || *opt_project == "" {
			log.Fatal("Error: -m parameter requires -p parameter to be set")
		}
	}

	projectFilter := ""
	if opt_project != nil && *opt_project != "" {
		projectFilter = *opt_project
	}

	showShellPath := opt_module != nil && *opt_module
	err = RunStatCommand(globalDB, projectFilter, showShellPath)
	if err != nil {
		log.Fatalf("Stat command failed: %v", err)
	}
}
