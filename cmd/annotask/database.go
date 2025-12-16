package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// InitGlobalDB initializes the global database
func InitGlobalDB(dbPath string) (*GlobalDB, error) {
	// Create directory if needed
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %v", err)
	}

	// Enable WAL mode and set busy_timeout for better concurrency
	// WAL mode allows multiple readers and one writer concurrently
	// busy_timeout makes SQLite wait up to 5 seconds for locks instead of failing immediately
	conn, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open global db: %v", err)
	}

	// Verify WAL mode is enabled (optional, for debugging)
	// If WAL mode cannot be enabled (e.g., on read-only filesystem), it will fall back to default
	var journalMode string
	err = conn.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err == nil {
		if journalMode != "wal" {
			log.Printf("Warning: WAL mode not enabled, using %s mode. Database may have lower concurrency performance.", journalMode)
		}
	}

	globalDB := &GlobalDB{Db: conn}

	// Create table
	sql_table := `
	CREATE TABLE IF NOT EXISTS tasks(
		Id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		usrID TEXT NOT NULL,
		project TEXT NOT NULL,
		module TEXT NOT NULL,
		mode TEXT NOT NULL,
		starttime datetime NOT NULL,
		endtime datetime,
		shellPath TEXT NOT NULL,
		totalTasks integer DEFAULT 0,
		pendingTasks integer DEFAULT 0,
		failedTasks integer DEFAULT 0,
		runningTasks integer DEFAULT 0,
		finishedTasks integer DEFAULT 0,
		status TEXT DEFAULT 'running',
		node TEXT,
		UNIQUE(usrID, project, module, starttime)
	);
	`
	_, err = conn.Exec(sql_table)
	if err != nil {
		return nil, fmt.Errorf("failed to create table: %v", err)
	}

	// Migrate: rename basename to module if exists
	var basenameExists bool
	err = conn.QueryRow("SELECT COUNT(*) FROM pragma_table_info('tasks') WHERE name='basename'").Scan(&basenameExists)
	if err == nil && basenameExists {
		_, err = conn.Exec("ALTER TABLE tasks RENAME COLUMN basename TO module")
		if err != nil {
			log.Printf("Warning: Could not rename basename to module: %v", err)
		}
	}

	// Migrate: add status column if it doesn't exist
	var statusExists bool
	err = conn.QueryRow("SELECT COUNT(*) FROM pragma_table_info('tasks') WHERE name='status'").Scan(&statusExists)
	if err == nil && !statusExists {
		_, err = conn.Exec("ALTER TABLE tasks ADD COLUMN status TEXT DEFAULT 'running'")
		if err != nil {
			log.Printf("Warning: Could not add status column: %v", err)
		}
	}

	// Migrate: add node column if it doesn't exist
	var nodeExists bool
	err = conn.QueryRow("SELECT COUNT(*) FROM pragma_table_info('tasks') WHERE name='node'").Scan(&nodeExists)
	if err == nil && !nodeExists {
		_, err = conn.Exec("ALTER TABLE tasks ADD COLUMN node TEXT")
		if err != nil {
			log.Printf("Warning: Could not add node column: %v", err)
		}
	}

	// Migrate: add pid column if it doesn't exist
	var pidExists bool
	err = conn.QueryRow("SELECT COUNT(*) FROM pragma_table_info('tasks') WHERE name='pid'").Scan(&pidExists)
	if err == nil && !pidExists {
		_, err = conn.Exec("ALTER TABLE tasks ADD COLUMN pid INTEGER")
		if err != nil {
			log.Printf("Warning: Could not add pid column: %v", err)
		}
	}

	_, err = conn.Exec(sql_table)
	if err != nil {
		return nil, fmt.Errorf("failed to create table: %v", err)
	}

	return globalDB, nil
}

func (sqObj *MySql) Crt_tb() {
	// create table if not exists
	// First, check if old table exists and migrate
	sqObj.migrateTable()

	sql_job_table := `
	CREATE TABLE IF NOT EXISTS job(
		Id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
		subJob_num INTEGER UNIQUE NOT NULL,
		shellPath TEXT,
		status TEXT,
		exitCode integer,
		retry integer DEFAULT 0,
		starttime datetime,
		endtime datetime,
		mode TEXT DEFAULT 'local',
		cpu integer DEFAULT 1,
		mem integer DEFAULT 1,
		h_vmem integer DEFAULT 1,
		taskid TEXT,
		node TEXT
	);
	`
	_, err := sqObj.Db.Exec(sql_job_table)
	if err != nil {
		panic(err)
	}
}

func (sqObj *MySql) migrateTable() {
	// Check if old table exists
	var tableName string
	err := sqObj.Db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='job'").Scan(&tableName)
	if err != nil {
		return // Table doesn't exist, no migration needed
	}

	// Check if taskid column was incorrectly used as primary key (old migration)
	var taskidAsPK bool
	err = sqObj.Db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('job') WHERE name='taskid' AND pk=1").Scan(&taskidAsPK)
	if err == nil && taskidAsPK {
		// Need to recreate table with correct structure
		// This is complex, so we'll just add taskid column if it doesn't exist
		log.Printf("Warning: Old schema detected. Please recreate database for full migration.")
	}

	// Add new columns if they don't exist
	columns := map[string]string{
		"mode":   "TEXT DEFAULT 'local'",
		"cpu":    "integer DEFAULT 1",
		"mem":    "integer DEFAULT 1",
		"h_vmem": "integer DEFAULT 1",
		"taskid": "TEXT",
		"node":   "TEXT",
	}

	for colName, colDef := range columns {
		var exists bool
		err = sqObj.Db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('job') WHERE name=?", colName).Scan(&exists)
		if err == nil && !exists {
			_, err = sqObj.Db.Exec(fmt.Sprintf("ALTER TABLE job ADD COLUMN %s %s", colName, colDef))
			if err != nil {
				log.Printf("Warning: Could not add column %s: %v", colName, err)
			}
		}
	}
}

func (sqObj *MySql) UpdateModeForUnfinished(mode JobMode) {
	_, err := sqObj.Db.Exec("UPDATE job SET mode=? WHERE status!=?", string(mode), J_finished)
	if err != nil {
		log.Printf("Warning: Could not update mode for unfinished jobs: %v", err)
	}
}

// CheckSignFilesAndUpdateStatus checks .sign files for all tasks and updates their status
// Tasks with .sign files are marked as finished, others are marked as pending
func CheckSignFilesAndUpdateStatus(dbObj *MySql) error {
	tx, err := dbObj.Db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// Query all tasks
	rows, err := tx.Query("SELECT subJob_num, shellPath, status FROM job")
	if err != nil {
		return fmt.Errorf("failed to query tasks: %v", err)
	}
	defer rows.Close()

	now := time.Now().Format("2006-01-02 15:04:05")
	var finishedCount int
	var pendingCount int

	for rows.Next() {
		var subJobNum int
		var shellPath string
		var currentStatus string

		err := rows.Scan(&subJobNum, &shellPath, &currentStatus)
		if err != nil {
			log.Printf("Warning: Failed to scan task: %v", err)
			continue
		}

		// Check if .sign file exists
		signFile := fmt.Sprintf("%s.sign", shellPath)
		if _, statErr := os.Stat(signFile); statErr == nil {
			// .sign file exists, task is finished
			if currentStatus != string(J_finished) {
				_, err = tx.Exec(`
					UPDATE job 
					SET status=?, endtime=?, exitCode=? 
					WHERE subJob_num=?
				`, J_finished, now, 0, subJobNum)
				if err != nil {
					log.Printf("Warning: Failed to update task %d to finished: %v", subJobNum, err)
				} else {
					finishedCount++
				}
			}
		} else {
			// .sign file doesn't exist, task should be pending
			// Update to pending regardless of current status (because .sign file is the source of truth)
			if currentStatus != string(J_pending) {
				_, err = tx.Exec(`
					UPDATE job 
					SET status=?, endtime=NULL, exitCode=NULL, taskid=NULL 
					WHERE subJob_num=?
				`, J_pending, subJobNum)
				if err != nil {
					log.Printf("Warning: Failed to update task %d to pending: %v", subJobNum, err)
				} else {
					pendingCount++
				}
			}
		}
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %v", err)
	}

	if finishedCount > 0 || pendingCount > 0 {
		log.Printf("Updated %d tasks to finished, %d tasks to pending based on .sign files", finishedCount, pendingCount)
	}

	return nil
}

func GetNeed2Run(dbObj *MySql) []int {
	tx, _ := dbObj.Db.Begin()
	defer tx.Rollback()

	rows, err := tx.Query("select subJob_num from job where Status!=?", J_finished)
	CheckErr(err)
	defer rows.Close()
	var subJob_num int

	need2run_N := CheckCount(rows)
	need2run := make([]int, need2run_N)

	ii := 0
	rows2, err := tx.Query("select subJob_num from job where Status!=?", J_finished)
	CheckErr(err)
	defer rows2.Close()
	for rows2.Next() {
		err = rows2.Scan(&subJob_num)
		CheckErr(err)
		need2run[ii] = subJob_num
		ii++
	}
	return need2run
}

// GetTaskStats gets task statistics from local database
func GetTaskStats(dbObj *MySql) (total, pending, failed, running, finished int, err error) {
	// Get total count
	err = dbObj.Db.QueryRow("SELECT COUNT(*) FROM job").Scan(&total)
	if err != nil {
		return
	}

	// Get status counts
	err = dbObj.Db.QueryRow("SELECT COUNT(*) FROM job WHERE status=?", J_pending).Scan(&pending)
	if err != nil {
		return
	}
	err = dbObj.Db.QueryRow("SELECT COUNT(*) FROM job WHERE status=?", J_failed).Scan(&failed)
	if err != nil {
		return
	}
	err = dbObj.Db.QueryRow("SELECT COUNT(*) FROM job WHERE status=?", J_running).Scan(&running)
	if err != nil {
		return
	}
	err = dbObj.Db.QueryRow("SELECT COUNT(*) FROM job WHERE status=?", J_finished).Scan(&finished)
	return
}

// UpdateGlobalTaskRecord updates or creates a task record in global database
// Uses transaction to ensure atomicity and prevent race conditions
func UpdateGlobalTaskRecord(globalDB *GlobalDB, usrID, project, module, mode, shellPath string, startTime time.Time, total, pending, failed, running, finished int, node string, pid int) error {
	startTimeStr := startTime.Format("2006-01-02 15:04:05")

	// Use transaction to ensure atomicity of UPDATE + INSERT operation
	tx, err := globalDB.Db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// Try to update existing record
	// Update node and pid fields as well to ensure they reflect the current run
	result, err := tx.Exec(`
		UPDATE tasks SET 
			pendingTasks=?, failedTasks=?, runningTasks=?, finishedTasks=?, totalTasks=?, node=?, pid=?
		WHERE usrID=? AND project=? AND module=? AND starttime=?
	`, pending, failed, running, finished, total, node, pid, usrID, project, module, startTimeStr)
	if err != nil {
		return fmt.Errorf("failed to update task record: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %v", err)
	}

	// If no rows affected, insert new record
	if rowsAffected == 0 {
		status := "running"
		if failed == 0 && running == 0 && pending == 0 && total > 0 {
			status = "completed"
		} else if failed > 0 {
			status = "failed"
		}
		// Use INSERT OR REPLACE to handle race condition where another process might have inserted
		// This is safer than plain INSERT when there's a UNIQUE constraint
		_, err = tx.Exec(`
			INSERT OR REPLACE INTO tasks(usrID, project, module, mode, starttime, shellPath, totalTasks, pendingTasks, failedTasks, runningTasks, finishedTasks, status, node, pid)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, usrID, project, module, mode, startTimeStr, shellPath, total, pending, failed, running, finished, status, node, pid)
		if err != nil {
			return fmt.Errorf("failed to insert task record: %v", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %v", err)
	}

	return nil
}

// UpdateGlobalTaskStatus updates the status field in global database
func UpdateGlobalTaskStatus(globalDB *GlobalDB, usrID, project, module string, startTime time.Time, status string) error {
	startTimeStr := startTime.Format("2006-01-02 15:04:05")
	_, err := globalDB.Db.Exec(`
		UPDATE tasks SET status=?
		WHERE usrID=? AND project=? AND module=? AND starttime=?
	`, status, usrID, project, module, startTimeStr)
	return err
}

// GetNodeName gets the node name based on mode
// For local mode, returns current hostname
// For qsubsge mode, returns current hostname (the node where annotask qsubsge is executed)
func GetNodeName(mode string, config *Config, dbObj *MySql) string {
	hostname, err := os.Hostname()
	if err != nil {
		log.Printf("Warning: Could not get hostname: %v", err)
		return "unknown"
	}

	if mode == "local" {
		return hostname
	} else if mode == "qsubsge" {
		// For qsubsge mode, record the node where annotask qsubsge is executed
		// This is the submission node, not the execution node
		// If config.Node is empty, record current node
		// If config.Node is not empty, current node must be in the list (checked by CheckNode)
		// so we record current node
		return hostname
	}
	return "unknown"
}
