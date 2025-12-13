package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/akamensky/argparse"
	_ "github.com/mattn/go-sqlite3"
)

// RunDeleteCommand runs the delete subcommand
func RunDeleteCommand(globalDB *GlobalDB, project, module string, taskID int) error {
	usrID := GetCurrentUserID()

	// First, query tasks to delete (get pid, mode, shellPath before deleting)
	var query string
	var rows *sql.Rows
	var err error

	if taskID > 0 {
		// Delete by task ID
		query = `
			SELECT pid, mode, shellPath, starttime, status
			FROM tasks
			WHERE usrID=? AND Id=?
		`
		rows, err = globalDB.Db.Query(query, usrID, taskID)
	} else if module != "" {
		query = `
			SELECT pid, mode, shellPath, starttime, status
			FROM tasks
			WHERE usrID=? AND project=? AND module=?
		`
		rows, err = globalDB.Db.Query(query, usrID, project, module)
	} else {
		query = `
			SELECT pid, mode, shellPath, starttime, status
			FROM tasks
			WHERE usrID=? AND project=?
		`
		rows, err = globalDB.Db.Query(query, usrID, project)
	}

	if err != nil {
		return fmt.Errorf("failed to query tasks: %v", err)
	}
	defer rows.Close()

	type TaskInfo struct {
		pid       sql.NullInt64
		mode      string
		shellPath string
		starttime string
		status    string
	}

	var tasksToDelete []TaskInfo
	for rows.Next() {
		var task TaskInfo
		err := rows.Scan(&task.pid, &task.mode, &task.shellPath, &task.starttime, &task.status)
		if err != nil {
			log.Printf("Warning: Failed to scan task info: %v", err)
			continue
		}
		tasksToDelete = append(tasksToDelete, task)
	}
	rows.Close()

	// Separate tasks by status: running tasks need full delete process, others just delete from DB
	var runningTasks []TaskInfo
	var nonRunningTasks []TaskInfo

	for _, task := range tasksToDelete {
		// Check if status is 'running' (case-insensitive comparison)
		if strings.ToLower(task.status) == "running" {
			runningTasks = append(runningTasks, task)
		} else {
			nonRunningTasks = append(nonRunningTasks, task)
		}
	}

	// For running tasks: execute full delete process (terminate processes, handle sub-tasks)
	for _, task := range runningTasks {
		// 1. Stop main process and its children by PID (only if process exists)
		if task.pid.Valid && task.pid.Int64 > 0 {
			pid := int(task.pid.Int64)
			// Check if process exists and is running before attempting to kill it
			if processExists(pid) {
				// Process exists, kill it with its children
				err := killProcessTree(pid)
				if err != nil {
					log.Printf("Warning: Failed to kill process tree for PID %d: %v", pid, err)
				} else {
					fmt.Printf("Terminated main process (PID: %d) and its children for module '%s'\n", pid, filepath.Base(task.shellPath))
				}
			}
			// If process doesn't exist, silently skip (no warning needed)
		}

		// 2. Handle sub-tasks based on mode
		if task.mode == "qsubsge" {
			// For qsubsge mode: delete SGE jobs using DRMAA and update status to failed
			err := stopQsubsgeTasks(task.shellPath)
			if err != nil {
				log.Printf("Warning: Failed to stop qsubsge tasks for %s: %v", task.shellPath, err)
			}
		} else if task.mode == "local" {
			// For local mode: update running tasks status to failed in local database
			err := updateLocalTasksStatusToFailed(task.shellPath)
			if err != nil {
				log.Printf("Warning: Failed to update local tasks status for %s: %v", task.shellPath, err)
			}
		}
	}

	// For non-running tasks: just delete from global database (no need to terminate processes)
	// They will be included in the DELETE query below along with running tasks

	// Now delete from global database (both running and non-running tasks)
	var result sql.Result
	if taskID > 0 {
		// Delete by task ID
		result, err = globalDB.Db.Exec(`
			DELETE FROM tasks
			WHERE usrID=? AND Id=?
		`, usrID, taskID)
		if err != nil {
			return fmt.Errorf("failed to delete tasks: %v", err)
		}
	} else if module != "" {
		// Delete specific project and module
		result, err = globalDB.Db.Exec(`
			DELETE FROM tasks
			WHERE usrID=? AND project=? AND module=?
		`, usrID, project, module)
		if err != nil {
			return fmt.Errorf("failed to delete tasks: %v", err)
		}
	} else {
		// Delete all tasks in project
		result, err = globalDB.Db.Exec(`
			DELETE FROM tasks
			WHERE usrID=? AND project=?
		`, usrID, project)
		if err != nil {
			return fmt.Errorf("failed to delete tasks: %v", err)
		}
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %v", err)
	}

	if taskID > 0 {
		fmt.Printf("Deleted %d task record(s) with ID %d\n", rowsAffected, taskID)
	} else if module != "" {
		fmt.Printf("Deleted %d task record(s) for project '%s' and module '%s'\n", rowsAffected, project, module)
	} else {
		fmt.Printf("Deleted %d task record(s) for project '%s'\n", rowsAffected, project)
	}

	return nil
}

// stopQsubsgeTasks stops running SGE jobs for qsubsge mode using DRMAA and updates status to failed
func stopQsubsgeTasks(shellPath string) error {
	// Open local database
	dbPath := shellPath + ".db"
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open local database: %v", err)
	}
	defer conn.Close()

	// Query running tasks with taskid (SGE job ID)
	rows, err := conn.Query(`
		SELECT subJob_num, taskid
		FROM job
		WHERE status=? AND taskid IS NOT NULL AND taskid != ''
	`, J_running)
	if err != nil {
		return fmt.Errorf("failed to query running jobs: %v", err)
	}
	defer rows.Close()

	type JobInfo struct {
		subJobNum int
		taskid    string
	}

	var jobs []JobInfo
	for rows.Next() {
		var job JobInfo
		err := rows.Scan(&job.subJobNum, &job.taskid)
		if err != nil {
			log.Printf("Warning: Failed to scan job: %v", err)
			continue
		}
		jobs = append(jobs, job)
	}

	if len(jobs) == 0 {
		return nil // No running jobs
	}

	// Delete SGE jobs using qdel command
	now := time.Now().Format("2006-01-02 15:04:05")
	for _, job := range jobs {
		// Use qdel command to terminate SGE job
		cmd := exec.Command("qdel", job.taskid)
		err := cmd.Run()
		if err != nil {
			log.Printf("Warning: Failed to terminate SGE job %s (task %d) using qdel: %v", job.taskid, job.subJobNum, err)
		} else {
			fmt.Printf("Terminated SGE job %s (task %d) using qdel\n", job.taskid, job.subJobNum)
		}

		// Update status to failed for this task
		_, err = conn.Exec(`
			UPDATE job SET status=?, endtime=?, exitCode=?
			WHERE subJob_num=?
		`, J_failed, now, 1, job.subJobNum)
		if err != nil {
			log.Printf("Warning: Failed to update status for task %d: %v", job.subJobNum, err)
		}
	}

	return nil
}

// updateLocalTasksStatusToFailed updates running tasks status to failed in local database for local mode
func updateLocalTasksStatusToFailed(shellPath string) error {
	// Open local database
	dbPath := shellPath + ".db"
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open local database: %v", err)
	}
	defer conn.Close()

	// Query running tasks
	rows, err := conn.Query(`
		SELECT subJob_num
		FROM job
		WHERE status=?
	`, J_running)
	if err != nil {
		return fmt.Errorf("failed to query running jobs: %v", err)
	}
	defer rows.Close()

	var subJobNums []int
	for rows.Next() {
		var subJobNum int
		err := rows.Scan(&subJobNum)
		if err != nil {
			log.Printf("Warning: Failed to scan job: %v", err)
			continue
		}
		subJobNums = append(subJobNums, subJobNum)
	}

	if len(subJobNums) == 0 {
		return nil // No running jobs
	}

	// Update status to failed for all running tasks
	now := time.Now().Format("2006-01-02 15:04:05")
	for _, subJobNum := range subJobNums {
		_, err = conn.Exec(`
			UPDATE job SET status=?, endtime=?, exitCode=?
			WHERE subJob_num=?
		`, J_failed, now, 1, subJobNum)
		if err != nil {
			log.Printf("Warning: Failed to update status for task %d: %v", subJobNum, err)
		} else {
			fmt.Printf("Updated task %d status to Failed\n", subJobNum)
		}
	}

	return nil
}

// processExists checks if a process with the given PID exists and is running
func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Send signal 0 to check if process exists (doesn't actually send a signal)
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// killProcessTree kills a process and all its children
func killProcessTree(pid int) error {
	// First check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process %d not found: %v", pid, err)
	}

	// Kill all children first (recursively, with depth limit)
	killProcessTreeHelper(pid, 10)

	// Kill the parent process (try SIGTERM first, then SIGKILL)
	err = process.Signal(syscall.SIGTERM)
	if err != nil {
		// Process might already be gone, try SIGKILL as fallback
		err = process.Kill()
		if err != nil {
			return fmt.Errorf("failed to kill process %d: %v", pid, err)
		}
	} else {
		// Wait a bit for graceful shutdown
		time.Sleep(200 * time.Millisecond)
		// Check if process still exists, if yes, force kill
		process, err := os.FindProcess(pid)
		if err == nil {
			process.Kill()
		}
	}

	return nil
}

// killProcessTreeHelper is a helper function to kill process tree with depth limit
func killProcessTreeHelper(pid int, maxDepth int) {
	if maxDepth <= 0 {
		return
	}

	// Find child processes
	cmd := exec.Command("pgrep", "-P", strconv.Itoa(pid))
	output, err := cmd.Output()
	if err != nil {
		return // No children or error
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		childPid, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		// Recursively kill descendants first
		killProcessTreeHelper(childPid, maxDepth-1)
		// Then kill this child process
		childProcess, err := os.FindProcess(childPid)
		if err == nil {
			childProcess.Signal(syscall.SIGTERM)
			time.Sleep(50 * time.Millisecond)
			childProcess.Kill()
		}
	}
}

// RunDeleteModule runs the delete module
func RunDeleteModule(config *Config, args []string) {
	// Initialize global DB
	globalDB, err := InitGlobalDB(config.Db)
	if err != nil {
		log.Fatalf("Failed to initialize global DB: %v", err)
	}
	defer globalDB.Db.Close()

	// Parse delete command arguments
	deleteParser := argparse.NewParser("annotask delete", "Delete task records from global database")
	opt_project := deleteParser.String("p", "project", &argparse.Options{Required: false, Help: "Project name"})
	opt_module := deleteParser.String("m", "module", &argparse.Options{Help: "Module (shell path basename without extension)"})
	opt_id := deleteParser.Int("k", "id", &argparse.Options{Help: "Task ID (from stat -p output)"})

	// Prepend program name for argparse.Parse (it expects os.Args-like format)
	parseArgs := append([]string{"annotask"}, args...)
	err = deleteParser.Parse(parseArgs)
	if err != nil {
		// If help is requested, show module help
		errStr := err.Error()
		if strings.Contains(strings.ToLower(errStr), "help") {
			printModuleHelp("delete", config)
			return
		}
		fmt.Print(deleteParser.Usage(err))
		os.Exit(1)
	}

	taskID := 0
	if opt_id != nil && *opt_id > 0 {
		taskID = *opt_id
		// When using -k/--id, project and module are not required
		err = RunDeleteCommand(globalDB, "", "", taskID)
		if err != nil {
			log.Fatalf("Delete command failed: %v", err)
		}
		return
	}

	// When not using -k/--id, project is required
	if opt_project == nil || *opt_project == "" {
		fmt.Print(deleteParser.Usage(fmt.Errorf("project name is required when not using -k/--id")))
		os.Exit(1)
	}

	module := ""
	if opt_module != nil && *opt_module != "" {
		module = *opt_module
	}

	err = RunDeleteCommand(globalDB, *opt_project, module, 0)
	if err != nil {
		log.Fatalf("Delete command failed: %v", err)
	}
}
