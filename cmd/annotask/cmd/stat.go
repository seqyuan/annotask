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
			SELECT project, module, mode, starttime, endtime, shellPath, 
			       totalTasks, pendingTasks, failedTasks, runningTasks, finishedTasks
			FROM tasks
			WHERE usrID=? AND project=?
			ORDER BY project, starttime DESC
		`, usrID, projectFilter)
	} else {
		rows, err = globalDB.Db.Query(`
			SELECT project, module, mode, starttime, endtime, shellPath, 
			       totalTasks, pendingTasks, failedTasks, runningTasks, finishedTasks
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
		var count int
		for rows.Next() {
			var project, module, mode, starttime, shellPath string
			var endtime sql.NullString
			var total, pending, failed, running, finished int

			err := rows.Scan(&project, &module, &mode, &starttime, &endtime, &shellPath,
				&total, &pending, &failed, &running, &finished)
			if err != nil {
				log.Printf("Error scanning row: %v", err)
				continue
			}

			fmt.Println(shellPath)
			count++
		}
		fmt.Printf("Total records: %d\n", count)
	} else {
		// Normal output with all columns
		fmt.Printf("Tasks for user: %s\n", usrID)
		if projectFilter != "" {
			fmt.Printf("Project filter: %s\n", projectFilter)
		}
		fmt.Println(strings.Repeat("-", 110))
		fmt.Printf("%-15s %-20s %-10s %-10s %-10s %-10s %-10s %-12s %-12s\n",
			"Project", "Module", "Mode", "Pending", "Failed", "Running", "Finished", "Start Time", "End Time")
		fmt.Println(strings.Repeat("-", 110))

		var count int
		for rows.Next() {
			var project, module, mode, starttime, shellPath string
			var endtime sql.NullString
			var total, pending, failed, running, finished int

			err := rows.Scan(&project, &module, &mode, &starttime, &endtime, &shellPath,
				&total, &pending, &failed, &running, &finished)
			if err != nil {
				log.Printf("Error scanning row: %v", err)
				continue
			}

			// Format time: remove year and seconds (MM-DD HH:MM)
			starttimeFormatted := formatTimeShort(starttime)
			endtimeStr := "-"
			if endtime.Valid {
				endtimeStr = formatTimeShort(endtime.String)
			}

			fmt.Printf("%-15s %-20s %-10s %-10d %-10d %-10d %-10d %-12s %-12s\n",
				project, module, mode, pending, failed, running, finished, starttimeFormatted, endtimeStr)
			count++
		}

		fmt.Println(strings.Repeat("-", 110))
		fmt.Printf("Total records: %d\n", count)
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

	err = statParser.Parse(args)
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

