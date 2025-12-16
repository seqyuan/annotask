package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/akamensky/argparse"
	_ "github.com/mattn/go-sqlite3"
)

// RunStatCommand runs the stat subcommand
func RunStatCommand(globalDB *GlobalDB, projectFilter string, config *Config) error {
	usrID := GetCurrentUserID()

	// First, query all tasks to update them before displaying
	var updateRows *sql.Rows
	var err error

	if projectFilter != "" {
		updateRows, err = globalDB.Db.Query(`
			SELECT shellPath, mode, project, module, starttime
			FROM tasks
			WHERE usrID=? AND project=?
		`, usrID, projectFilter)
	} else {
		updateRows, err = globalDB.Db.Query(`
			SELECT shellPath, mode, project, module, starttime
			FROM tasks
			WHERE usrID=?
		`, usrID)
	}

	if err == nil {
		defer updateRows.Close()
		// Update each task's status from local database
		for updateRows.Next() {
			var shellPath, mode, project, module, starttime string
			err := updateRows.Scan(&shellPath, &mode, &project, &module, &starttime)
			if err != nil {
				log.Printf("Warning: Failed to scan task for update: %v", err)
				continue
			}

			// Parse starttime (support multiple formats: "2006-01-02 15:04:05" and ISO 8601 formats)
			var startTime time.Time
			formats := []string{
				"2006-01-02 15:04:05",
				time.RFC3339,     // "2006-01-02T15:04:05Z07:00"
				"2006-01-02T15:04:05Z",
				"2006-01-02T15:04:05",
			}
			parsed := false
			for _, format := range formats {
				if t, err := time.Parse(format, starttime); err == nil {
					startTime = t
					parsed = true
					break
				}
			}
			if !parsed {
				// If parsing fails, skip this record silently
				// formatTimeShort will handle the formatting with fallback logic
				continue
			}

			// Open local database
			dbPath := shellPath + ".db"
			conn, err := sql.Open("sqlite3", dbPath)
			if err != nil {
				// Local database doesn't exist or can't be opened, skip
				continue
			}

			dbObj := &MySql{Db: conn}
			// Get task statistics from local database
			total, pending, failed, running, finished, err := GetTaskStats(dbObj)
			conn.Close()

			if err != nil {
				log.Printf("Warning: Failed to get task stats for %s: %v", shellPath, err)
				continue
			}

			// Get node name
			// For local mode: get current hostname
			// For qsubsge mode: get from database (submission node)
			node := "-"
			if mode == "local" {
				hostname, err := os.Hostname()
				if err == nil {
					node = hostname
				}
			} else if mode == "qsubsge" {
				// For qsubsge mode, node is stored in database (submission node)
				var nodeValue sql.NullString
				err = globalDB.Db.QueryRow(`
					SELECT node FROM tasks 
					WHERE usrID=? AND project=? AND module=? AND starttime=?
				`, usrID, project, module, starttime).Scan(&nodeValue)
				if err == nil && nodeValue.Valid && nodeValue.String != "" {
					node = nodeValue.String
				}
			}

			// Get PID if process is still running (try to get from global DB first)
			var pid int
			var pidValue sql.NullInt64
			err = globalDB.Db.QueryRow(`
				SELECT pid FROM tasks 
				WHERE usrID=? AND project=? AND module=? AND starttime=?
			`, usrID, project, module, starttime).Scan(&pidValue)
			if err == nil && pidValue.Valid {
				pid = int(pidValue.Int64)
			} else {
				// If not found in DB, check if process exists (optional, could be 0)
				pid = 0
			}

			// Update global database
			err = UpdateGlobalTaskRecord(globalDB, usrID, project, module, mode, shellPath, startTime, total, pending, failed, running, finished, node, pid)
			if err != nil {
				log.Printf("Warning: Failed to update task record for %s: %v", shellPath, err)
			}
		}
		updateRows.Close()
	}

	var rows *sql.Rows

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
			id        int
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

			// Store id and shellPath for later output
			modules = append(modules, struct {
				id        int
				shellPath string
			}{id: id, shellPath: shellPath})
		}

		// Then output id and shell paths for each module: id shellPath
		if len(modules) > 0 {
			fmt.Println() // Empty line separator
			for _, m := range modules {
				fmt.Printf("%d %s\n", m.id, m.shellPath)
			}
		}
	} else {
		// When no -p, show: project module mode status statis stime etime
		rows, err = globalDB.Db.Query(`
			SELECT project, module, mode, status, totalTasks, finishedTasks, starttime, endtime
			FROM tasks
			WHERE usrID=?
			ORDER BY project, starttime DESC
		`, usrID)
		if err != nil {
			return fmt.Errorf("failed to query tasks: %v", err)
		}
		defer rows.Close()

		// Output format: project module mode status statis stime etime
		// statis format: finishedTasks/totalTasks (已完成数/总任务数)
		fmt.Printf("%-15s %-20s %-10s %-10s %-15s %-12s %-12s\n",
			"project", "module", "mode", "status", "statis", "stime", "etime")

		var count int
		for rows.Next() {
			var project, module, mode, starttime string
			var status sql.NullString
			var totalTasks, finishedTasks int
			var endtime sql.NullString

			err := rows.Scan(&project, &module, &mode, &status, &totalTasks, &finishedTasks, &starttime, &endtime)
			if err != nil {
				log.Printf("Error scanning row: %v", err)
				continue
			}

			// Format status
			statusStr := "-"
			if status.Valid {
				statusStr = status.String
			}

			// Format statis: finishedTasks/totalTasks (已完成数/总任务数)
			statisStr := fmt.Sprintf("%d/%d", finishedTasks, totalTasks)

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

	err = RunStatCommand(globalDB, projectFilter, config)
	if err != nil {
		log.Fatalf("Stat command failed: %v", err)
	}
}
