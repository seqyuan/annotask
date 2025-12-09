package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dgruber/drmaa"
	"github.com/seqyuan/annotask/pkg/gpool"
)

// Global DRMAA session manager
var (
	drmaaSession     drmaa.Session
	drmaaSessionMutex sync.Mutex
	drmaaSessionInit  bool
)

// getDRMAASession returns a global DRMAA session (thread-safe)
func getDRMAASession() (*drmaa.Session, error) {
	drmaaSessionMutex.Lock()
	defer drmaaSessionMutex.Unlock()

	if !drmaaSessionInit {
		session, err := drmaa.MakeSession()
		if err != nil {
			return nil, err
		}
		drmaaSession = session
		drmaaSessionInit = true
	}

	return &drmaaSession, nil
}

// closeDRMAASession closes the global DRMAA session (should be called at program exit)
func closeDRMAASession() {
	drmaaSessionMutex.Lock()
	defer drmaaSessionMutex.Unlock()

	if drmaaSessionInit {
		drmaaSession.Exit()
		drmaaSessionInit = false
	}
}

func IlterCommand(ctx context.Context, dbObj *MySql, thred int, need2run []int, mode JobMode, cpu, mem, h_vmem int, userSetMem, userSetHvmem bool, queue string, sgeProject string, write_pool *gpool.Pool) {
	pool := gpool.New(thred)

	for _, N := range need2run {
		pool.Add(1)
		if mode == ModeQsubSge {
			go SubmitQsubCommand(ctx, N, pool, dbObj, write_pool, cpu, mem, h_vmem, userSetMem, userSetHvmem, queue, sgeProject)
		} else {
			go RunCommand(N, pool, dbObj, write_pool)
		}
	}

	// Wait for all goroutines to complete
	// write_pool.Wait() is called at runTasks level to ensure all operations complete
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

func SubmitQsubCommand(ctx context.Context, N int, pool *gpool.Pool, dbObj *MySql, write_pool *gpool.Pool, cpu, mem, h_vmem int, userSetMem, userSetHvmem bool, queue string, sgeProject string) {
	defer pool.Done()

	var subShellPath string
	var retry int
	var currentMem int
	var currentHvmem int
	var taskid sql.NullString
	err := dbObj.Db.QueryRow("select shellPath, retry, mem, h_vmem, taskid from job where subJob_num = ?", N).Scan(&subShellPath, &retry, &currentMem, &currentHvmem, &taskid)
	CheckErr(err)

	// If retry > 0, use stored memory values (may have been increased)
	// Only use stored values if user originally set the corresponding parameter
	// This ensures we use the previously increased values for subsequent retries
	if retry > 0 {
		// Only update mem if user set it originally
		if userSetMem && currentMem > 0 {
			mem = currentMem
		}
		// Only update h_vmem if user set it originally
		if userSetHvmem && currentHvmem > 0 {
			h_vmem = currentHvmem
		}
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	write_pool.Add(1)
	_, err = dbObj.Db.Exec("UPDATE job set status=?, starttime=?, cpu=?, mem=?, h_vmem=? where subJob_num=?", J_running, now, cpu, mem, h_vmem, N)
	CheckErr(err)
	write_pool.Done()

	// Get global DRMAA session (thread-safe)
	session, err := getDRMAASession()
	if err != nil {
		write_pool.Add(1)
		now = time.Now().Format("2006-01-02 15:04:05")
		retry++
		drmaaErr := err // Save original error before database update
		_, dbErr := dbObj.Db.Exec("UPDATE job set status=?, endtime=?, exitCode=?, retry=? where subJob_num=?", J_failed, now, 1, retry, N)
		write_pool.Done()
		if dbErr != nil {
			log.Printf("Error updating database: %v", dbErr)
		}
		if drmaaErr != nil {
			log.Printf("Error getting DRMAA session: %v", drmaaErr)
		} else {
			log.Printf("Error getting DRMAA session: unknown error (err is nil)")
		}
		return
	}
	// Note: We don't defer session.Exit() here because it's a global session
	// that should remain open for the lifetime of the program

	// Create job template
	jt, err := session.AllocateJobTemplate()
	if err != nil {
		write_pool.Add(1)
		now = time.Now().Format("2006-01-02 15:04:05")
		retry++
		templateErr := err // Save original error before database update
		_, dbErr := dbObj.Db.Exec("UPDATE job set status=?, endtime=?, exitCode=?, retry=? where subJob_num=?", J_failed, now, 1, retry, N)
		write_pool.Done()
		if dbErr != nil {
			log.Printf("Error updating database: %v", dbErr)
		}
		if templateErr != nil {
			log.Printf("Error allocating job template: %v", templateErr)
		} else {
			log.Printf("Error allocating job template: unknown error (err is nil)")
		}
		return
	}
	defer session.DeleteJobTemplate(&jt)

	// Get directory and base name of subShellPath
	subShellDir := filepath.Dir(subShellPath)
	subShellBase := filepath.Base(subShellPath)
	subShellBaseNoExt := strings.TrimSuffix(subShellBase, filepath.Ext(subShellBase))

	// Set job template properties
	// Note: SetRemoteCommand sets the script path, which SGE will use as the command to execute
	// The working directory will be automatically set to the script's directory by SGE
	jt.SetRemoteCommand(subShellPath)
	// Set job name to file prefix, so SGE will auto-generate output files as:
	// {subShellBaseNoExt}.o.{jobID} and {subShellBaseNoExt}.e.{jobID}
	jt.SetJobName(subShellBaseNoExt)

	// Build nativeSpec with only SGE resource options
	// Do NOT include -cwd or script path in nativeSpec
	// - SetRemoteCommand already sets the script path, which SGE uses to determine working directory
	// - SGE automatically uses the script's directory as the working directory
	// - Output files will be generated in the script's directory: {job_name}.o.{jobID} and {job_name}.e.{jobID}
	// - Including -cwd in nativeSpec may cause parsing errors with some DRMAA implementations
	nativeSpec := fmt.Sprintf("-l cpu=%d", cpu)
	if userSetMem {
		nativeSpec += fmt.Sprintf(" -l mem=%dG", mem)
	}
	if userSetHvmem {
		nativeSpec += fmt.Sprintf(" -l h_vmem=%dG", h_vmem)
	}
	// Add queue specification if provided (supports multiple queues, comma-separated)
	if queue != "" {
		nativeSpec += fmt.Sprintf(" -q %s", queue)
	}
	// Add SGE project specification if provided (for resource quota management)
	if sgeProject != "" {
		nativeSpec += fmt.Sprintf(" -P %s", sgeProject)
	}
	jt.SetNativeSpecification(nativeSpec)
	
	// Debug: log nativeSpec for troubleshooting
	log.Printf("DEBUG: Task %d - nativeSpec: %s, subShellPath: %s, subShellDir: %s", N, nativeSpec, subShellPath, subShellDir)

	// Submit job
	jobID, err := session.RunJob(&jt)
	if err != nil {
		write_pool.Add(1)
		now = time.Now().Format("2006-01-02 15:04:05")
		retry++
		submitErr := err // Save original error before database update
		_, dbErr := dbObj.Db.Exec("UPDATE job set status=?, endtime=?, exitCode=?, retry=? where subJob_num=?", J_failed, now, 1, retry, N)
		write_pool.Done()
		if dbErr != nil {
			log.Printf("Error updating database: %v", dbErr)
		}
		if submitErr != nil {
			log.Printf("Error submitting job: %v", submitErr)
		} else {
			log.Printf("Error submitting job: unknown error (err is nil)")
		}
		return
	}

	// Store SGE job ID as taskid for qsubsge mode
	write_pool.Add(1)
	_, err = dbObj.Db.Exec("UPDATE job set taskid=? where subJob_num=?", jobID, N)
	write_pool.Done()
	CheckErr(err)

	// Monitor job status
	for {
		// Check if context is cancelled (should not happen normally, but allows graceful shutdown)
		select {
		case <-ctx.Done():
			// Context cancelled, exit monitoring loop
			// Note: The job will continue running on SGE, we just stop monitoring
			// The job status will be checked again in the next retry round
			log.Printf("Context cancelled for job %d (jobID: %s), stopping monitoring. Job continues on SGE.", N, jobID)
			return
		default:
			// Continue monitoring
		}

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
			// Job finished, check exit code and get execution node
			var exitCode int = 0
			var isMemoryError bool = false
			var executionNode string = ""

			// Try to get execution node from DRMAA JobInfo
			jobInfo, err := session.Wait(jobID, drmaa.TimeoutNoWait)
			if err == nil {
				// Try to get execution node from ResourceUsage
				resourceUsage := jobInfo.ResourceUsage()
				if host, ok := resourceUsage["exec_host"]; ok {
					// exec_host format might be "node1/1" or "node1", extract node name
					executionNode = strings.Split(host, "/")[0]
				} else if host, ok := resourceUsage["hostname"]; ok {
					executionNode = host
				} else {
					// Try using qstat command as fallback
					cmd := exec.Command("qstat", "-j", jobID)
					output, err := cmd.Output()
					if err == nil {
						lines := strings.Split(string(output), "\n")
						for _, line := range lines {
							if strings.HasPrefix(line, "exec_host") {
								parts := strings.Fields(line)
								if len(parts) >= 2 {
									// Format: exec_host  node1/1 or exec_host         node1/1
									executionNode = strings.Split(parts[len(parts)-1], "/")[0]
									break
								}
							}
						}
					}
				}
			}

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
			if _, statErr := os.Stat(signFile); statErr == nil {
				exitCode = 0
			} else {
				if !isMemoryError {
					if state == drmaa.PsFailed {
						exitCode = 1
					} else if err == nil && jobInfo.HasExited() {
						// err here refers to the err from session.Wait() call above
						exitCode = int(jobInfo.ExitStatus())
					} else {
						exitCode = 1
					}
				}
			}

			write_pool.Add(1)
			now = time.Now().Format("2006-01-02 15:04:05")
			if exitCode == 0 {
				_, err = dbObj.Db.Exec("UPDATE job set status=?, endtime=?, exitCode=?, node=? where subJob_num=?", J_finished, now, exitCode, executionNode, N)
			} else {
				retry++
				newMem := mem
				newHvmem := h_vmem
				if isMemoryError {
					// Increase memory by 125% only if user set the corresponding parameter
					// Round up to ensure we have enough memory
					if userSetMem {
						newMem = int(math.Ceil(float64(mem) * 1.25))
					}
					if userSetHvmem {
						newHvmem = int(math.Ceil(float64(h_vmem) * 1.25))
					}
				}
				_, err = dbObj.Db.Exec("UPDATE job set status=?, endtime=?, exitCode=?, retry=?, mem=?, h_vmem=?, node=? where subJob_num=?", J_failed, now, exitCode, retry, newMem, newHvmem, executionNode, N)
			}
			write_pool.Done()
			CheckErr(err)
			return
		} else if state == drmaa.PsRunning {
			// Job is running, try to get execution node if not already stored
			var currentNode sql.NullString
			err := dbObj.Db.QueryRow("SELECT node FROM job WHERE subJob_num=?", N).Scan(&currentNode)
			if err == nil && (!currentNode.Valid || currentNode.String == "") {
				// Try to get execution node using qstat command
				cmd := exec.Command("qstat", "-j", jobID)
				output, err := cmd.Output()
				if err == nil {
					lines := strings.Split(string(output), "\n")
					var executionNode string
					for _, line := range lines {
						if strings.HasPrefix(line, "exec_host") {
							parts := strings.Fields(line)
							if len(parts) >= 2 {
								// Format: exec_host  node1/1 or exec_host         node1/1
								executionNode = strings.Split(parts[len(parts)-1], "/")[0]
								break
							}
						}
					}
					if executionNode != "" {
						write_pool.Add(1)
						_, err = dbObj.Db.Exec("UPDATE job set node=? where subJob_num=?", executionNode, N)
						write_pool.Done()
						if err != nil {
							log.Printf("Warning: Could not update execution node: %v", err)
						}
					}
				}
			}
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

	rows0, err := tx.Query("select exitCode from job where exitCode=0")
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
