package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"
)

// MonitorTaskStatus monitors database and outputs task status changes to stdout
func MonitorTaskStatus(ctx context.Context, dbObj *MySql, globalDB *GlobalDB, usrID, project, module, mode, shellPath string, startTime time.Time, config *Config, wg *sync.WaitGroup) {
	defer wg.Done()

	// Map to track last known status for each task
	lastStatus := make(map[int]TaskStatus)
	headerPrinted := false // Track if header has been printed
	
	// Use configurable update interval (default: 5 seconds)
	// This reduces database load and lock contention when many processes are running
	updateInterval := 5 // Default fallback
	if config != nil && config.MonitorUpdateInterval > 0 {
		updateInterval = config.MonitorUpdateInterval
	}
	ticker := time.NewTicker(time.Duration(updateInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Get current try round (MAX(retry) + 1, because initial run has retry=0)
			var maxRetry int
			err := dbObj.Db.QueryRow("SELECT COALESCE(MAX(retry), -1) FROM job").Scan(&maxRetry)
			if err != nil {
				log.Printf("Error querying max retry: %v", err)
			}
			currentRound := maxRetry + 1 // 0 -> 1, 1 -> 2, 2 -> 3
			maxRetries := config.Retry.Max

			// Query all tasks
			rows, err := dbObj.Db.Query(`
				SELECT subJob_num, status, retry, taskid, starttime, endtime, exitCode 
				FROM job 
				ORDER BY subJob_num
			`)
			if err != nil {
				log.Printf("Error querying task status: %v", err)
				continue
			}

			// Track current status
			currentStatus := make(map[int]TaskStatus)

			// Print header on first iteration
			if !headerPrinted {
				printTaskHeader(maxRetries)
				headerPrinted = true
			}

			for rows.Next() {
				var ts TaskStatus
				err := rows.Scan(&ts.subJobNum, &ts.status, &ts.retry, &ts.taskid, &ts.starttime, &ts.endtime, &ts.exitCode)
				if err != nil {
					log.Printf("Error scanning task status: %v", err)
					continue
				}
				currentStatus[ts.subJobNum] = ts

				// Skip output for Pending status
				if ts.status == "Pending" {
					continue
				}

				// Check if status changed
				last, exists := lastStatus[ts.subJobNum]
				if !exists {
					// New task, output initial status
					outputTaskStatus(ts, currentRound, maxRetries)
				} else {
					// Check if status, retry, or other fields changed
					if last.status != ts.status || last.retry != ts.retry ||
						last.taskid.String != ts.taskid.String ||
						(ts.endtime.Valid && (!last.endtime.Valid || last.endtime.String != ts.endtime.String)) {
						outputTaskStatus(ts, currentRound, maxRetries)
					}
				}
			}
			rows.Close()

			// Update global database
			if globalDB != nil && config != nil {
				total, pending, failed, running, finished, err := GetTaskStats(dbObj)
				if err == nil {
					node := GetNodeName(mode, config, dbObj)
					err = UpdateGlobalTaskRecord(globalDB, usrID, project, module, mode, shellPath, startTime, total, pending, failed, running, finished, node)
					if err != nil {
						log.Printf("Error updating global DB: %v", err)
					}
				}
			}

			// Update lastStatus
			lastStatus = currentStatus
		}
	}
}

// printTaskHeader prints the table header for task status output
func printTaskHeader(maxRetries int) {
	fmt.Printf("%-6s %-6s %-10s %-10s %-8s %-12s\n", "try", "task", "status", "taskid", "exitcode", "time")
}

// outputTaskStatus outputs task status to stdout in table format
func outputTaskStatus(ts TaskStatus, currentRound int, maxRetries int) {
	// Format try column (current round : max retries), e.g., "1:3", "2:3", "3:3"
	tryStr := fmt.Sprintf("%d:%d", currentRound, maxRetries)

	// Format task number as 4-digit zero-padded (e.g., 0001, 0002)
	taskNumStr := fmt.Sprintf("%04d", ts.subJobNum)

	// Format taskid
	var taskidStr string
	if ts.taskid.Valid {
		taskidStr = ts.taskid.String
	} else {
		taskidStr = "-"
	}

	// Format exit code
	var exitCodeStr string
	if ts.exitCode.Valid {
		exitCodeStr = strconv.FormatInt(ts.exitCode.Int64, 10)
	} else {
		exitCodeStr = "-"
	}

	// Format time using formatTimeShort (MM-DD HH:MM format)
	var timeStr string
	if ts.endtime.Valid {
		timeStr = formatTimeShort(ts.endtime.String)
	} else if ts.starttime.Valid {
		timeStr = formatTimeShort(ts.starttime.String)
	} else {
		timeStr = "-"
	}

	// Output in table format: try task status taskid exitcode time
	fmt.Printf("%-6s %-6s %-10s %-10s %-8s %-12s\n",
		tryStr, taskNumStr, ts.status, taskidStr, exitCodeStr, timeStr)
}
