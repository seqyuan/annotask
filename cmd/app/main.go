package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/akamensky/argparse"
	"github.com/dgruber/drmaa"
	_ "github.com/mattn/go-sqlite3"
	"github.com/seqyuan/annotask/pkg/gpool"
	"gopkg.in/yaml.v3"
)

type MySql struct {
	Db *sql.DB
}

type Config struct {
	Db      string `yaml:"db"`
	Project string `yaml:"project"`
	Retry   struct {
		Max int `yaml:"max"`
	} `yaml:"retry"`
	Queue    string `yaml:"queue"`
	Node     string `yaml:"node"`
	Defaults struct {
		Line   int `yaml:"line"`
		Thread int `yaml:"thread"`
		CPU    int `yaml:"cpu"`
		Mem    int `yaml:"mem"`
	} `yaml:"defaults"`
}

type GlobalDB struct {
	Db *sql.DB
}

// LoadConfig loads configuration from annotask.yaml
func LoadConfig() (*Config, error) {
	// Get executable directory
	exePath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	exeDir := filepath.Dir(exePath)
	configPath := filepath.Join(exeDir, "annotask.yaml")

	config := &Config{
		Db:      filepath.Join(exeDir, "annotask.db"),
		Project: "default",
	}
	config.Retry.Max = 3
	config.Queue = "default.q"
	config.Node = ""
	config.Defaults.Line = 1
	config.Defaults.Thread = 1
	config.Defaults.CPU = 1
	config.Defaults.Mem = 1

	// Try to load from file
	if _, err := os.Stat(configPath); err == nil {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %v", err)
		}
		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %v", err)
		}
	} else {
		// Create default config file
		data, err := yaml.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal default config: %v", err)
		}
		if err := os.WriteFile(configPath, data, 0644); err != nil {
			log.Printf("Warning: Could not write default config file: %v", err)
		}
	}

	// If node is empty, get current hostname
	if config.Node == "" {
		hostname, err := os.Hostname()
		if err != nil {
			log.Printf("Warning: Could not get hostname: %v", err)
			config.Node = "unknown"
		} else {
			config.Node = hostname
		}
	}

	return config, nil
}

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
	_, err = conn.Exec(sql_table)
	if err != nil {
		return nil, fmt.Errorf("failed to create table: %v", err)
	}

	return globalDB, nil
}

// GetCurrentUserID returns current user ID
func GetCurrentUserID() string {
	u, err := user.Current()
	if err != nil {
		return "unknown"
	}
	return u.Username
}

// CheckNode checks if current node matches config node (for qsubsge mode)
func CheckNode(configNode string) error {
	currentNode, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to get hostname: %v", err)
	}
	if currentNode != configNode {
		return fmt.Errorf("current node (%s) does not match config node (%s)", currentNode, configNode)
	}
	return nil
}

type JobMode string

const (
	ModeLocal   JobMode = "local"
	ModeQsubSge JobMode = "qsubsge"
)

type jobStatusType string

const (
	J_pending  jobStatusType = "Pending"
	J_failed   jobStatusType = "Failed"
	J_running  jobStatusType = "Running"
	J_finished jobStatusType = "Finished"
)

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
		taskid TEXT
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

func CheckCount(rows *sql.Rows) (count int) {
	count = 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		panic(err)
	}
	return count
}

func GenerateShell(shellPath, content string) {
	fi, err := os.Create(shellPath)
	if err != nil {
		panic(err)
	}
	defer fi.Close()

	content = strings.TrimRight(content, "\n")
	content = fmt.Sprintf("#!/bin/bash\necho ========== start at : `date +%%Y/%%m/%%d %%H:%%M:%%S` ==========\n%s", content)
	content = fmt.Sprintf("%s && \\\necho ========== end at : `date +%%Y/%%m/%%d %%H:%%M:%%S` ========== && \\\n", content)
	content = fmt.Sprintf("%secho LLAP 1>&2 && \\\necho LLAP > %s.sign\n", content, shellPath)

	_, err = fi.Write([]byte(content))
	if err != nil {
		panic(err)
	}
	err = os.Chmod(shellPath, 0755)
	if err != nil {
		panic(err)
	}
}

func getFilePrefix(filePath string) string {
	base := filepath.Base(filePath)
	ext := filepath.Ext(base)
	if ext != "" {
		return base[:len(base)-len(ext)]
	}
	return base
}

func Creat_tb(shell_path string, line_unit int, mode JobMode) (dbObj *MySql) {
	shellAbsName, _ := filepath.Abs(shell_path)
	dbpath := shellAbsName + ".db"
	subShellPath := shellAbsName + ".shell"

	err := os.MkdirAll(subShellPath, 0777)
	CheckErr(err)

	conn, err := sql.Open("sqlite3", dbpath)
	CheckErr(err)
	dbObj = &MySql{Db: conn}
	dbObj.Crt_tb()

	// Update mode for unfinished jobs
	dbObj.UpdateModeForUnfinished(mode)

	// Get file prefix for naming
	filePrefix := getFilePrefix(shellAbsName)

	tx, _ := dbObj.Db.Begin()
	defer tx.Rollback()
	insert_job, err := tx.Prepare("INSERT INTO job(subJob_num, shellPath, status, retry, mode) values(?,?,?,?,?)")
	CheckErr(err)

	f, err := os.Open(shellAbsName)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	buf := bufio.NewReader(f)

	ii := 0
	var cmd_l string = ""
	N := 0
	for {
		line, err := buf.ReadString('\n')
		if err != nil || err == io.EOF {
			break
		}

		if ii == 0 {
			cmd_l = line
			ii++
		} else if ii < line_unit {
			cmd_l = cmd_l + line
			ii++
		} else {
			N++
			Nrows, err := tx.Query("select Id from job where subJob_num = ?", N)
			if err != nil {
				CheckErr(err)
			}
			defer Nrows.Close()
			if CheckCount(Nrows) == 0 {
				cmd_l = strings.TrimRight(cmd_l, "\n")
				subShell := fmt.Sprintf("%s/%s_%04d.sh", subShellPath, filePrefix, N)
				GenerateShell(subShell, cmd_l)
				_, _ = insert_job.Exec(N, subShell, J_pending, 0, string(mode))
			}

			ii = 1
			cmd_l = line
		}
	}

	if ii > 0 {
		N++
		Nrows, err := tx.Query("select Id from job where subJob_num = ?", N)
		if err != nil {
			CheckErr(err)
		}
		defer Nrows.Close()
		if CheckCount(Nrows) == 0 {
			cmd_l = strings.TrimRight(cmd_l, "\n")
			subShell := fmt.Sprintf("%s/%s_%04d.sh", subShellPath, filePrefix, N)
			GenerateShell(subShell, cmd_l)
			_, _ = insert_job.Exec(N, subShell, J_pending, 0, string(mode))
		}
	}

	err = tx.Commit()
	CheckErr(err)
	return
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

func IlterCommand(dbObj *MySql, thred int, need2run []int, mode JobMode, cpu, mem, h_vmem int) {
	pool := gpool.New(thred)
	write_pool := gpool.New(1)

	for _, N := range need2run {
		pool.Add(1)
		if mode == ModeQsubSge {
			go SubmitQsubCommand(N, pool, dbObj, write_pool, cpu, mem, h_vmem)
		} else {
			go RunCommand(N, pool, dbObj, write_pool)
		}
	}

	write_pool.Wait()
	pool.Wait()
}

func RunCommand(N int, pool *gpool.Pool, dbObj *MySql, write_pool *gpool.Pool) {
	defer pool.Done()

	var subShellPath string
	var retry int
	err := dbObj.Db.QueryRow("select shellPath, retry from job where subJob_num = ?", N).Scan(&subShellPath, &retry)
	CheckErr(err)

	now := time.Now().Format("2006-01-02 15:04:05")
	write_pool.Add(1)
	_, err = dbObj.Db.Exec("UPDATE job set status=?, starttime=? where subJob_num=?", J_running, now, N)
	CheckErr(err)
	write_pool.Done()

	defaultFailedCode := 1
	cmd := exec.Command("sh", subShellPath)
	// 其他程序stdout stderr改到当前目录pwd
	sho, err := os.OpenFile(fmt.Sprintf("%s.o", subShellPath), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0755)
	CheckErr(err)
	defer sho.Close()
	she, err := os.OpenFile(fmt.Sprintf("%s.e", subShellPath), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0755)
	CheckErr(err)
	defer she.Close()
	Owriter := io.MultiWriter(sho)
	Ewriter := io.MultiWriter(she)
	cmd.Stdout = Owriter
	cmd.Stderr = Ewriter

	err = cmd.Start() // Start the process
	if err != nil {
		write_pool.Add(1)
		now = time.Now().Format("2006-01-02 15:04:05")
		retry++
		_, err = dbObj.Db.Exec("UPDATE job set status=?, endtime=?, exitCode=?, retry=? where subJob_num=?", J_failed, now, 1, retry, N)
		write_pool.Done()
		CheckErr(err)
		return
	}

	// Store PID as taskid for local mode
	write_pool.Add(1)
	_, err2 := dbObj.Db.Exec("UPDATE job set taskid=? where subJob_num=?", strconv.Itoa(cmd.Process.Pid), N)
	if err2 != nil {
		log.Printf("Warning: Could not update taskid: %v", err2)
	}
	write_pool.Done()

	err = cmd.Wait() // Wait for process to complete

	var exitCode int

	if err != nil {
		// try to get the exit code
		if exitError, ok := err.(*exec.ExitError); ok {
			ws := exitError.Sys().(syscall.WaitStatus)
			exitCode = ws.ExitStatus()
		} else {
			exitCode = defaultFailedCode
		}
	} else {
		// success, exitCode should be 0 if go is ok
		ws := cmd.ProcessState.Sys().(syscall.WaitStatus)
		exitCode = ws.ExitStatus()
	}

	write_pool.Add(1)
	now = time.Now().Format("2006-01-02 15:04:05")
	if exitCode == 0 {
		_, err = dbObj.Db.Exec("UPDATE job set status=?, endtime=?, exitCode=? where subJob_num=?", J_finished, now, exitCode, N)
	} else {
		// Check if process is still running (for retry logic)
		retry++
		_, err = dbObj.Db.Exec("UPDATE job set status=?, endtime=?, exitCode=?, retry=? where subJob_num=?", J_failed, now, exitCode, retry, N)
		// Retry logic will be handled by main loop
	}

	write_pool.Done()
	CheckErr(err)
}

func SubmitQsubCommand(N int, pool *gpool.Pool, dbObj *MySql, write_pool *gpool.Pool, cpu, mem, h_vmem int) {
	defer pool.Done()

	var subShellPath string
	var retry int
	var currentMem int
	var currentHvmem int
	var taskid sql.NullString
	err := dbObj.Db.QueryRow("select shellPath, retry, mem, h_vmem, taskid from job where subJob_num = ?", N).Scan(&subShellPath, &retry, &currentMem, &currentHvmem, &taskid)
	CheckErr(err)

	// If retry > 0, use stored memory values (may have been increased)
	if retry > 0 && currentMem > 0 {
		mem = currentMem
		h_vmem = currentHvmem
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	write_pool.Add(1)
	_, err = dbObj.Db.Exec("UPDATE job set status=?, starttime=?, cpu=?, mem=?, h_vmem=? where subJob_num=?", J_running, now, cpu, mem, h_vmem, N)
	CheckErr(err)
	write_pool.Done()

	// Initialize DRMAA session
	session, err := drmaa.MakeSession()
	if err != nil {
		write_pool.Add(1)
		now = time.Now().Format("2006-01-02 15:04:05")
		retry++
		_, err = dbObj.Db.Exec("UPDATE job set status=?, endtime=?, exitCode=?, retry=? where subJob_num=?", J_failed, now, 1, retry, N)
		write_pool.Done()
		log.Printf("Error creating DRMAA session: %v", err)
		return
	}
	defer session.Exit()

	// Create job template
	jt, err := session.AllocateJobTemplate()
	if err != nil {
		write_pool.Add(1)
		now = time.Now().Format("2006-01-02 15:04:05")
		retry++
		_, err = dbObj.Db.Exec("UPDATE job set status=?, endtime=?, exitCode=?, retry=? where subJob_num=?", J_failed, now, 1, retry, N)
		write_pool.Done()
		log.Printf("Error allocating job template: %v", err)
		return
	}
	defer session.DeleteJobTemplate(&jt)

	// Get directory and base name of subShellPath
	subShellDir := filepath.Dir(subShellPath)
	subShellBase := filepath.Base(subShellPath)
	subShellBaseNoExt := strings.TrimSuffix(subShellBase, filepath.Ext(subShellBase))

	// Set job template properties
	jt.SetRemoteCommand(subShellPath)
	// Set job name to file prefix, so SGE will auto-generate output files as:
	// {subShellBaseNoExt}.o.{jobID} and {subShellBaseNoExt}.e.{jobID}
	jt.SetJobName(subShellBaseNoExt)

	// Set resource requirements and working directory
	// -cwd sets the working directory to subShellPath's directory
	// SGE will automatically generate output files in the working directory:
	// {job_name}.o.{jobID} and {job_name}.e.{jobID}
	nativeSpec := fmt.Sprintf("-cwd %s -l cpu=%d -l mem=%dG -l h_vmem=%dG", subShellDir, cpu, mem, h_vmem)
	jt.SetNativeSpecification(nativeSpec)

	// Submit job
	jobID, err := session.RunJob(&jt)
	if err != nil {
		write_pool.Add(1)
		now = time.Now().Format("2006-01-02 15:04:05")
		retry++
		_, err = dbObj.Db.Exec("UPDATE job set status=?, endtime=?, exitCode=?, retry=? where subJob_num=?", J_failed, now, 1, retry, N)
		write_pool.Done()
		log.Printf("Error submitting job: %v", err)
		return
	}

	// Store SGE job ID as taskid for qsubsge mode
	write_pool.Add(1)
	_, err = dbObj.Db.Exec("UPDATE job set taskid=? where subJob_num=?", jobID, N)
	write_pool.Done()
	CheckErr(err)

	// Monitor job status
	for {
		time.Sleep(5 * time.Second)

		// Check job status using DRMAA
		state, err := session.JobPs(jobID)
		if err != nil {
			log.Printf("Error checking job status: %v", err)
			// Try to determine status from files
			state = drmaa.PsDone
		}

		// Check if job is finished
		if state == drmaa.PsDone || state == drmaa.PsFailed {
			// Job finished, check exit code
			var exitCode int = 0
			var isMemoryError bool = false

			// Build actual file paths using jobID (taskid)
			// Get directory and base name of subShellPath
			subShellDir := filepath.Dir(subShellPath)
			subShellBase := filepath.Base(subShellPath)
			subShellBaseNoExt := strings.TrimSuffix(subShellBase, filepath.Ext(subShellBase))

			// Check error file for memory-related errors
			// Error file path: subShellDir/subShellBaseNoExt.e.jobID
			errFile := filepath.Join(subShellDir, fmt.Sprintf("%s.e.%s", subShellBaseNoExt, jobID))
			if errData, readErr := os.ReadFile(errFile); readErr == nil {
				errStr := string(errData)
				errStrLower := strings.ToLower(errStr)
				if strings.Contains(errStrLower, "killed") || strings.Contains(errStrLower, "memory") ||
					strings.Contains(errStrLower, "h_vmem") || strings.Contains(errStrLower, "out of memory") ||
					strings.Contains(errStrLower, "oom") {
					isMemoryError = true
					exitCode = 137 // Typical exit code for OOM kills
				}
			}

			// Check if .sign file exists (success indicator)
			// Sign file is created by the shell script itself, path is still subShellPath.sign
			signFile := fmt.Sprintf("%s.sign", subShellPath)
			if _, err := os.Stat(signFile); err == nil {
				exitCode = 0
			} else {
				if !isMemoryError {
					if state == drmaa.PsFailed {
						exitCode = 1
					} else {
						// Try to get exit code from DRMAA
						jobInfo, err := session.Wait(jobID, drmaa.TimeoutNoWait)
						if err == nil && jobInfo.HasExited() {
							exitCode = int(jobInfo.ExitStatus())
						} else {
							exitCode = 1
						}
					}
				}
			}

			write_pool.Add(1)
			now = time.Now().Format("2006-01-02 15:04:05")
			if exitCode == 0 {
				_, err = dbObj.Db.Exec("UPDATE job set status=?, endtime=?, exitCode=? where subJob_num=?", J_finished, now, exitCode, N)
			} else {
				retry++
				newMem := mem
				newHvmem := h_vmem
				if isMemoryError {
					// Increase memory by 125%
					newMem = int(float64(mem) * 1.25)
					newHvmem = int(float64(h_vmem) * 1.25)
				}
				_, err = dbObj.Db.Exec("UPDATE job set status=?, endtime=?, exitCode=?, retry=?, mem=?, h_vmem=? where subJob_num=?", J_failed, now, exitCode, retry, newMem, newHvmem, N)
			}
			write_pool.Done()
			CheckErr(err)
			return
		}
		// Job is still running, continue monitoring
	}
}

func CheckExitCode(dbObj *MySql) {
	tx, _ := dbObj.Db.Begin()
	defer tx.Rollback()

	rows1, err := tx.Query("select subJob_num, shellPath from job where exitCode!=0")
	CheckErr(err)
	defer rows1.Close()
	rows12, err := tx.Query("select subJob_num, shellPath from job where exitCode!=0")
	CheckErr(err)
	defer rows12.Close()

	rows0, err := tx.Query("select exitCode from job where exitCode==0")
	CheckErr(err)
	defer rows0.Close()

	SuccessCount := CheckCount(rows0)
	ErrorCount := CheckCount(rows1)

	exitCode := 0
	os.Stderr.WriteString(fmt.Sprintf("All works: %v\nSuccessed: %v\nError: %v\n", SuccessCount+ErrorCount, SuccessCount, ErrorCount))
	if ErrorCount > 0 {
		exitCode = 1
		os.Stderr.WriteString("Err Shells:\n")
	}

	var subJob_num int
	var shellPath string
	for rows12.Next() {
		err := rows12.Scan(&subJob_num, &shellPath)
		CheckErr(err)
		os.Stderr.WriteString(fmt.Sprintf("%v\t%s\n", subJob_num, shellPath))
	}

	os.Exit(exitCode)
}

var documents string = `任务并发程序 parallel task v1.5.0`

func CheckErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// TaskStatus represents the current status of a task
type TaskStatus struct {
	subJobNum int
	status    string
	retry     int
	taskid    sql.NullString
	starttime sql.NullString
	endtime   sql.NullString
	exitCode  sql.NullInt64
}

// UpdateGlobalDB updates the global database with task statistics
func UpdateGlobalDB(globalDB *GlobalDB, usrID, project, module, mode, shellPath string, startTime time.Time) error {
	// Get task statistics from local database
	// This function should be called with a reference to the local dbObj
	// For now, we'll create a helper that takes the db path
	return nil
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
func UpdateGlobalTaskRecord(globalDB *GlobalDB, usrID, project, module, mode, shellPath string, startTime time.Time, total, pending, failed, running, finished int) error {
	startTimeStr := startTime.Format("2006-01-02 15:04:05")

	// Try to update existing record
	result, err := globalDB.Db.Exec(`
		UPDATE tasks SET 
			pendingTasks=?, failedTasks=?, runningTasks=?, finishedTasks=?, totalTasks=?
		WHERE usrID=? AND project=? AND module=? AND starttime=?
	`, pending, failed, running, finished, total, usrID, project, module, startTimeStr)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// If no rows affected, insert new record
	if rowsAffected == 0 {
		_, err = globalDB.Db.Exec(`
			INSERT INTO tasks(usrID, project, module, mode, starttime, shellPath, totalTasks, pendingTasks, failedTasks, runningTasks, finishedTasks)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, usrID, project, module, mode, startTimeStr, shellPath, total, pending, failed, running, finished)
		return err
	}

	return nil
}

// MonitorTaskStatus monitors database and outputs task status changes to stdout
func MonitorTaskStatus(ctx context.Context, dbObj *MySql, globalDB *GlobalDB, usrID, project, module, mode, shellPath string, startTime time.Time, wg *sync.WaitGroup) {
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
			if globalDB != nil {
				total, pending, failed, running, finished, err := GetTaskStats(dbObj)
				if err == nil {
					err = UpdateGlobalTaskRecord(globalDB, usrID, project, module, mode, shellPath, startTime, total, pending, failed, running, finished)
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

// RunDeleteCommand runs the delete subcommand
func RunDeleteCommand(globalDB *GlobalDB, project, module string) error {
	usrID := GetCurrentUserID()

	var result sql.Result
	var err error

	if module != "" {
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

	if module != "" {
		fmt.Printf("Deleted %d task(s) for project '%s' and module '%s'\n", rowsAffected, project, module)
	} else {
		fmt.Printf("Deleted %d task(s) for project '%s'\n", rowsAffected, project)
	}

	return nil
}

// formatTimeShort formats time string to remove year and seconds
// Input: "2006-01-02 15:04:05" -> Output: "01-02 15:04"
func formatTimeShort(timeStr string) string {
	if timeStr == "" || timeStr == "-" {
		return "-"
	}
	// Parse the time string
	t, err := time.Parse("2006-01-02 15:04:05", timeStr)
	if err != nil {
		// If parsing fails, try to extract manually
		parts := strings.Fields(timeStr)
		if len(parts) >= 2 {
			dateParts := strings.Split(parts[0], "-")
			timeParts := strings.Split(parts[1], ":")
			if len(dateParts) >= 3 && len(timeParts) >= 2 {
				return fmt.Sprintf("%s-%s %s:%s", dateParts[1], dateParts[2], timeParts[0], timeParts[1])
			}
		}
		return timeStr
	}
	// Format without year and seconds
	return t.Format("01-02 15:04")
}

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

func main() {
	// Load configuration
	config, err := LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Check subcommands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "stat":
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

			err = statParser.Parse(os.Args[2:])
			if err != nil {
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
			return

		case "delete":
			// Initialize global DB
			globalDB, err := InitGlobalDB(config.Db)
			if err != nil {
				log.Fatalf("Failed to initialize global DB: %v", err)
			}
			defer globalDB.Db.Close()

			// Parse delete command arguments
			deleteParser := argparse.NewParser("annotask delete", "Delete task records from global database")
			opt_project := deleteParser.String("p", "project", &argparse.Options{Required: true, Help: "Project name (required)"})
			opt_module := deleteParser.String("m", "module", &argparse.Options{Help: "Module (shell path basename without extension)"})

			err = deleteParser.Parse(os.Args[2:])
			if err != nil {
				fmt.Print(deleteParser.Usage(err))
				os.Exit(1)
			}

			module := ""
			if opt_module != nil && *opt_module != "" {
				module = *opt_module
			}

			err = RunDeleteCommand(globalDB, *opt_project, module)
			if err != nil {
				log.Fatalf("Delete command failed: %v", err)
			}
			return

		case "qsubsge":
			// QsubSge mode as subcommand
			runQsubSgeMode(config)
			return
		}
	}

	// Main command (local mode)
	runLocalMode(config)
}

// runLocalMode runs tasks in local mode
func runLocalMode(config *Config) {
	parser := argparse.NewParser("annotask", documents)
	opt_i := parser.String("i", "infile", &argparse.Options{Required: true, Help: "Input shell command file (one command per line or grouped by -l)"})
	opt_l := parser.Int("l", "line", &argparse.Options{Default: config.Defaults.Line, Help: fmt.Sprintf("Number of lines to group as one task (default: %d)", config.Defaults.Line)})
	opt_p := parser.Int("p", "thread", &argparse.Options{Default: config.Defaults.Thread, Help: fmt.Sprintf("Max concurrent tasks to run (default: %d)", config.Defaults.Thread)})
	opt_project := parser.String("", "project", &argparse.Options{Default: config.Project, Help: fmt.Sprintf("Project name (default: %s)", config.Project)})

	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Print(parser.Usage(err))
		return
	}

	// For local mode, h_vmem is not used, but we still need to pass a value
	// Use mem * 1.25 as default (though it won't be used in local mode)
	h_vmem := int(float64(config.Defaults.Mem) * 1.25)
	runTasks(config, *opt_i, *opt_l, *opt_p, *opt_project, ModeLocal, config.Defaults.CPU, config.Defaults.Mem, h_vmem)
}

// runQsubSgeMode runs tasks in qsubsge mode
func runQsubSgeMode(config *Config) {
	// Check node
	if err := CheckNode(config.Node); err != nil {
		log.Fatalf("Node check failed: %v", err)
	}

	parser := argparse.NewParser("annotask qsubsge", "Submit tasks to qsub SGE system")
	opt_i := parser.String("i", "infile", &argparse.Options{Required: true, Help: "Input shell command file (one command per line or grouped by -l)"})
	opt_l := parser.Int("l", "line", &argparse.Options{Default: config.Defaults.Line, Help: fmt.Sprintf("Number of lines to group as one task (default: %d)", config.Defaults.Line)})
	opt_p := parser.Int("p", "thread", &argparse.Options{Default: config.Defaults.Thread, Help: fmt.Sprintf("Max concurrent tasks to run (default: %d)", config.Defaults.Thread)})
	opt_project := parser.String("", "project", &argparse.Options{Default: config.Project, Help: fmt.Sprintf("Project name (default: %s)", config.Project)})
	opt_cpu := parser.Int("", "cpu", &argparse.Options{Default: config.Defaults.CPU, Help: fmt.Sprintf("Number of CPUs per task (default: %d)", config.Defaults.CPU)})
	opt_mem := parser.Int("", "mem", &argparse.Options{Default: config.Defaults.Mem, Help: fmt.Sprintf("Memory in GB per task (default: %d)", config.Defaults.Mem)})
	opt_h_vmem := parser.Int("", "h_vmem", &argparse.Options{Required: false, Help: "Virtual memory in GB per task (default: mem * 1.25 if not set)"})

	err := parser.Parse(os.Args[2:])
	if err != nil {
		fmt.Print(parser.Usage(err))
		os.Exit(1)
	}

	// If h_vmem is not set (0), calculate as mem * 1.25
	h_vmem := *opt_h_vmem
	if h_vmem == 0 {
		h_vmem = int(float64(*opt_mem) * 1.25)
	}

	runTasks(config, *opt_i, *opt_l, *opt_p, *opt_project, ModeQsubSge, *opt_cpu, *opt_mem, h_vmem)
}

// runTasks is the common function to run tasks in both modes
func runTasks(config *Config, infile string, line, thread int, project string, mode JobMode, cpu, mem, h_vmem int) {

	// Initialize global DB
	globalDB, err := InitGlobalDB(config.Db)
	if err != nil {
		log.Fatalf("Failed to initialize global DB: %v", err)
	}
	defer globalDB.Db.Close()

	// Get user ID and prepare task info
	usrID := GetCurrentUserID()
	shellAbsPath, _ := filepath.Abs(infile)
	module := getFilePrefix(shellAbsPath)
	startTime := time.Now()

	dbObj := Creat_tb(infile, line, mode)
	need2run := GetNeed2Run(dbObj)
	fmt.Println(need2run)

	// Start task status monitor goroutine
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go MonitorTaskStatus(ctx, dbObj, globalDB, usrID, project, module, string(mode), shellAbsPath, startTime, &wg)

	// Retry loop for failed tasks
	maxRetries := config.Retry.Max
	for retryCount := 0; retryCount < maxRetries; retryCount++ {
		IlterCommand(dbObj, thread, need2run, mode, cpu, mem, h_vmem)
		need2run = GetNeed2Run(dbObj)
		if len(need2run) == 0 {
			break
		}
		fmt.Printf("Retry round %d: %d tasks to retry\n", retryCount+1, len(need2run))
		time.Sleep(2 * time.Second)
	}

	// Stop the monitor goroutine
	cancel()
	wg.Wait()

	// Final update to global DB
	endTime := time.Now()
	total, pending, failed, running, finished, _ := GetTaskStats(dbObj)
	UpdateGlobalTaskRecord(globalDB, usrID, project, module, string(mode), shellAbsPath, startTime, total, pending, failed, running, finished)
	// Update endtime
	startTimeStr := startTime.Format("2006-01-02 15:04:05")
	endTimeStr := endTime.Format("2006-01-02 15:04:05")
	globalDB.Db.Exec("UPDATE tasks SET endtime=? WHERE usrID=? AND project=? AND module=? AND starttime=?",
		endTimeStr, usrID, project, module, startTimeStr)

	CheckExitCode(dbObj)
}
