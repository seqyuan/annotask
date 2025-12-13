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
func RunStatCommand(globalDB *GlobalDB, projectFilter string) error {
	usrID := GetCurrentUserID()

	var rows *sql.Rows
	var err error

	if projectFilter != "" {
		// When -p is used, show different format: id module pending running failed finished stime etime
		rows, err = globalDB.Db.Query(`
			SELECT Id, module, pendingTasks, runningTasks, failedTasks, finishedTasks, starttime, endtime, shellPath
			FROM tasks
			WHERE usrID=? AND project=?
			ORDER BY starttime DESC
		`, usrID, projectFilter)
	if err != nil {
		return fmt.Errorf("failed to query tasks: %v", err)
	}
	defer rows.Close()

		// First output table: id module pending running failed finished stime etime
		fmt.Printf("%-6s %-20s %-8s %-8s %-8s %-9s %-12s %-12s\n",
			"id", "module", "pending", "running", "failed", "finished", "stime", "etime")

		var modules []struct {
			module    string
			shellPath string
		}

		for rows.Next() {
			var id int
			var module, starttime, shellPath string
			var pending, running, failed, finished int
			var endtime sql.NullString

			err := rows.Scan(&id, &module, &pending, &running, &failed, &finished, &starttime, &endtime, &shellPath)
			if err != nil {
				log.Printf("Error scanning row: %v", err)
				continue
			}

			// Format starttime and endtime: MM-DD HH:MM
			stimeStr := formatTimeShort(starttime)
			etimeStr := "-"
			if endtime.Valid {
				etimeStr = formatTimeShort(endtime.String)
			}

			fmt.Printf("%-6d %-20s %-8d %-8d %-8d %-9d %-12s %-12s\n",
				id, module, pending, running, failed, finished, stimeStr, etimeStr)

			// Store module and shellPath for later output
			modules = append(modules, struct {
				module    string
				shellPath string
			}{module: module, shellPath: shellPath})
		}

		// Then output shell paths for each module: module_shellPath
		if len(modules) > 0 {
			fmt.Println() // Empty line separator
			for _, m := range modules {
				fmt.Printf("%s_%s\n", m.module, m.shellPath)
			}
		}
	} else {
		// When no -p, show: project module mode status statis stime etime
		rows, err = globalDB.Db.Query(`
			SELECT project, module, mode, status, totalTasks, pendingTasks, starttime, endtime
			FROM tasks
			WHERE usrID=?
			ORDER BY project, starttime DESC
		`, usrID)
		if err != nil {
			return fmt.Errorf("failed to query tasks: %v", err)
		}
		defer rows.Close()

		// Output format: project module mode status statis stime etime
		// statis format: totalTasks/pendingTasks
		fmt.Printf("%-15s %-20s %-10s %-10s %-15s %-12s %-12s\n",
			"project", "module", "mode", "status", "statis", "stime", "etime")

		var count int
		for rows.Next() {
			var project, module, mode, starttime string
			var status sql.NullString
			var totalTasks, pendingTasks int
			var endtime sql.NullString

			err := rows.Scan(&project, &module, &mode, &status, &totalTasks, &pendingTasks, &starttime, &endtime)
			if err != nil {
				log.Printf("Error scanning row: %v", err)
				continue
			}

			// Format status
			statusStr := "-"
			if status.Valid {
				statusStr = status.String
			}

			// Format statis: totalTasks/pendingTasks
			statisStr := fmt.Sprintf("%d/%d", totalTasks, pendingTasks)

			// Format starttime and endtime: MM-DD HH:MM
			stimeStr := formatTimeShort(starttime)
			etimeStr := "-"
			if endtime.Valid {
				etimeStr = formatTimeShort(endtime.String)
			}

			fmt.Printf("%-15s %-20s %-10s %-10s %-15s %-12s %-12s\n",
				project, module, mode, statusStr, statisStr, stimeStr, etimeStr)
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

	projectFilter := ""
	if opt_project != nil && *opt_project != "" {
		projectFilter = *opt_project
	}

	err = RunStatCommand(globalDB, projectFilter)
	if err != nil {
		log.Fatalf("Stat command failed: %v", err)
	}
}
