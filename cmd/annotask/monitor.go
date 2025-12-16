package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"
)

// MonitorTaskStatus monitors database and outputs task status changes to log file
func MonitorTaskStatus(ctx context.Context, dbObj *MySql, globalDB *GlobalDB, usrID, project, module, mode, shellPath string, startTime time.Time, config *Config, wg *sync.WaitGroup, command string) {
	defer wg.Done()

	// Open log file: shellPath.log (e.g., test.sh.log)
	logFilePath := shellPath + ".log"
	
	// Check if file exists to determine if we should write command header
	fileExists := false
	if _, err := os.Stat(logFilePath); err == nil {
		fileExists = true
	}
	
	logFile, err := os.OpenFile(logFilePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("Error opening log file %s: %v", logFilePath, err)
		return
	}
	defer logFile.Close()

	// Mutex to protect concurrent writes to log file
	var logMutex sync.Mutex
	
	// Write command at the beginning if file is new (doesn't exist before)
	// If file exists, append command with newline separator
	if command != "" {
		logMutex.Lock()
		if fileExists {
			// File exists, append newline before command
			fmt.Fprintf(logFile, "\n")
		}
		fmt.Fprintf(logFile, "%s\n", command)
		fmt.Fprintf(logFile, "\n")
		logFile.Sync()
		logMutex.Unlock()
	}

	// Map to track last known status for each task
	lastStatus := make(map[int]TaskStatus)
	headerPrinted := false // Track if header has been printed

	// Log file update interval: real-time updates (short interval, e.g., 2 seconds)
	// {script}.log is only written by one process, so it can update frequently
	logUpdateInterval := 2 // seconds
	logTicker := time.NewTicker(time.Duration(logUpdateInterval) * time.Second)
	defer logTicker.Stop()

	// Global database update interval: controlled by monitor_update_interval
	// This reduces database load and lock contention when many processes are running
	globalDBUpdateInterval := 60 // Default fallback
	if config != nil && config.MonitorUpdateInterval > 0 {
		globalDBUpdateInterval = config.MonitorUpdateInterval
	}
	globalDBTicker := time.NewTicker(time.Duration(globalDBUpdateInterval) * time.Second)
	defer globalDBTicker.Stop()

	// Helper function to update log file with current task status
	updateLogFile := func() {
		// Get current try round (MAX(retry) + 1, because initial run has retry=0)
		var maxRetry int
		err := dbObj.Db.QueryRow("SELECT COALESCE(MAX(retry), -1) FROM job").Scan(&maxRetry)
		if err != nil {
			log.Printf("Error querying max retry: %v", err)
			return
		}
		currentRound := maxRetry + 1 // 0 -> 1, 1 -> 2, 2 -> 3
		maxRetries := 3              // Default fallback
		if config != nil {
			maxRetries = config.Retry.Max
		}

		// Query all tasks
		rows, err := dbObj.Db.Query(`
			SELECT subJob_num, status, retry, taskid, starttime, endtime, exitCode 
			FROM job 
			ORDER BY subJob_num
		`)
		if err != nil {
			log.Printf("Error querying task status: %v", err)
			return
		}
		defer rows.Close()

		// Track current status
		currentStatus := make(map[int]TaskStatus)

		// Print header on first iteration
		if !headerPrinted {
			printTaskHeader(logFile, &logMutex, maxRetries)
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
				outputTaskStatus(logFile, &logMutex, ts, currentRound, maxRetries)
			} else {
				// Check if status, retry, or other fields changed
				if last.status != ts.status || last.retry != ts.retry ||
					last.taskid.String != ts.taskid.String ||
					(ts.endtime.Valid && (!last.endtime.Valid || last.endtime.String != ts.endtime.String)) {
					outputTaskStatus(logFile, &logMutex, ts, currentRound, maxRetries)
				}
			}
		}

		// Update lastStatus
		lastStatus = currentStatus
	}

	// Helper function to update global database
	updateGlobalDB := func() {
		if globalDB != nil && config != nil {
			total, pending, failed, running, finished, err := GetTaskStats(dbObj)
			if err == nil {
				node := GetNodeName(mode, config, dbObj)
				pid := os.Getpid() // Get main process PID
				err = UpdateGlobalTaskRecord(globalDB, usrID, project, module, mode, shellPath, startTime, total, pending, failed, running, finished, node, pid)
				if err != nil {
					log.Printf("Error updating global DB: %v", err)
				}
			}
		}
	}

	// Perform initial update immediately for log file
	updateLogFile()

	// Perform initial update for global database immediately
	// (The record should already exist from runTasks, but we update it to ensure it's current)
	updateGlobalDB()

	for {
		select {
		case <-ctx.Done():
			return
		case <-logTicker.C:
			// Update log file in real-time
			updateLogFile()
		case <-globalDBTicker.C:
			// Update global database at configured interval
			updateGlobalDB()
		}
	}
}

// printTaskHeader prints the table header for task status output to log file
func printTaskHeader(logFile *os.File, logMutex *sync.Mutex, maxRetries int) {
	logMutex.Lock()
	defer logMutex.Unlock()
	fmt.Fprintf(logFile, "%-6s %-6s %-10s %-10s %-8s %-12s\n", "try", "task", "status", "taskid", "exitcode", "time")
	logFile.Sync() // Force flush to disk for real-time visibility
}

// outputTaskStatus outputs task status to log file in table format
func outputTaskStatus(logFile *os.File, logMutex *sync.Mutex, ts TaskStatus, currentRound int, maxRetries int) {
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
	logMutex.Lock()
	defer logMutex.Unlock()
	fmt.Fprintf(logFile, "%-6s %-6s %-10s %-10s %-8s %-12s\n",
		tryStr, taskNumStr, ts.status, taskidStr, exitCodeStr, timeStr)
	logFile.Sync() // Force flush to disk for real-time visibility
}
