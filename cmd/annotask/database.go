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

	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open global db: %v", err)
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
func UpdateGlobalTaskRecord(globalDB *GlobalDB, usrID, project, module, mode, shellPath string, startTime time.Time, total, pending, failed, running, finished int, node string) error {
	startTimeStr := startTime.Format("2006-01-02 15:04:05")

	// Try to update existing record
	// Update node field as well to ensure it reflects the current run's node (especially for local mode)
	result, err := globalDB.Db.Exec(`
		UPDATE tasks SET 
			pendingTasks=?, failedTasks=?, runningTasks=?, finishedTasks=?, totalTasks=?, node=?
		WHERE usrID=? AND project=? AND module=? AND starttime=?
	`, pending, failed, running, finished, total, node, usrID, project, module, startTimeStr)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// If no rows affected, insert new record
	if rowsAffected == 0 {
		status := "running"
		if failed == 0 && running == 0 && pending == 0 && total > 0 {
			status = "completed"
		} else if failed > 0 {
			status = "failed"
		}
		_, err = globalDB.Db.Exec(`
			INSERT INTO tasks(usrID, project, module, mode, starttime, shellPath, totalTasks, pendingTasks, failedTasks, runningTasks, finishedTasks, status, node)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, usrID, project, module, mode, startTimeStr, shellPath, total, pending, failed, running, finished, status, node)
		return err
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
// For qsubsge mode, returns "-" because tasks may run on multiple different nodes
func GetNodeName(mode string, config *Config, dbObj *MySql) string {
	if mode == "local" {
		hostname, err := os.Hostname()
		if err != nil {
			log.Printf("Warning: Could not get hostname: %v", err)
			return "unknown"
		}
		return hostname
	} else if mode == "qsubsge" {
		// For qsubsge mode, don't record node in global database
		// because tasks may run on multiple different nodes
		return "-"
	}
	return "unknown"
}
