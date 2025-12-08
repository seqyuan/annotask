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
	ticker := time.NewTicker(1 * time.Second) // Check every second
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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

			for rows.Next() {
				var ts TaskStatus
				err := rows.Scan(&ts.subJobNum, &ts.status, &ts.retry, &ts.taskid, &ts.starttime, &ts.endtime, &ts.exitCode)
				if err != nil {
					log.Printf("Error scanning task status: %v", err)
					continue
				}
				currentStatus[ts.subJobNum] = ts

				// Check if status changed
				last, exists := lastStatus[ts.subJobNum]
				if !exists {
					// New task, output initial status
					outputTaskStatus(ts, true)
				} else {
					// Check if status, retry, or other fields changed
					if last.status != ts.status || last.retry != ts.retry ||
						last.taskid.String != ts.taskid.String ||
						(ts.endtime.Valid && (!last.endtime.Valid || last.endtime.String != ts.endtime.String)) {
						outputTaskStatus(ts, false)
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

// outputTaskStatus outputs task status to stdout
func outputTaskStatus(ts TaskStatus, isNew bool) {
	var taskidStr string
	if ts.taskid.Valid {
		taskidStr = ts.taskid.String
	} else {
		taskidStr = "-"
	}

	var exitCodeStr string
	if ts.exitCode.Valid {
		exitCodeStr = strconv.FormatInt(ts.exitCode.Int64, 10)
	} else {
		exitCodeStr = "-"
	}

	var timeStr string
	if ts.endtime.Valid {
		timeStr = ts.endtime.String
	} else if ts.starttime.Valid {
		timeStr = ts.starttime.String
	} else {
		timeStr = "-"
	}

	prefix := ""
	if isNew {
		prefix = "[NEW] "
	} else if ts.retry > 0 {
		prefix = fmt.Sprintf("[RETRY-%d] ", ts.retry)
	}

	// Format: [PREFIX] Task #N: Status=STATUS, Retry=RETRY, TaskID=TASKID, ExitCode=EXITCODE, Time=TIME
	fmt.Printf("%sTask #%d: Status=%s, Retry=%d, TaskID=%s, ExitCode=%s, Time=%s\n",
		prefix, ts.subJobNum, ts.status, ts.retry, taskidStr, exitCodeStr, timeStr)
}

