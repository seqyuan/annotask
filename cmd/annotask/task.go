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
	drmaaSession      drmaa.Session
	drmaaSessionMutex sync.Mutex
	drmaaSessionInit  bool
	// configuredSettingsPath stores the settings.sh path from config
	// Set by runQsubSgeMode before first DRMAA session creation
	configuredSettingsPath string
)

// loadSettingsSh loads environment variables from SGE settings.sh file
// This mimics the effect of "source settings.sh" in shell
func loadSettingsSh(settingsPath string) error {
	// Read settings.sh file
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("failed to read settings.sh: %v", err)
	}

	lines := strings.Split(string(content), "\n")
	sgeRoot := ""

	// First pass: extract SGE_ROOT and simple variable assignments
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Extract SGE_ROOT first (needed for other commands)
		if strings.HasPrefix(line, "SGE_ROOT=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				// Remove quotes and semicolon
				value := strings.Trim(parts[1], `";'`)
				value = strings.TrimSuffix(value, ";")
				sgeRoot = value
				os.Setenv("SGE_ROOT", sgeRoot)
			}
		}
	}

	// If SGE_ROOT not found in file, return error
	if sgeRoot == "" {
		return fmt.Errorf("SGE_ROOT not found in settings.sh")
	}

	// Verify SGE_ROOT path exists
	if _, err := os.Stat(sgeRoot); err != nil {
		return fmt.Errorf("SGE_ROOT path does not exist: %s", sgeRoot)
	}

	// Second pass: extract other environment variables and execute commands
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Handle SGE_CELL
		if strings.HasPrefix(line, "SGE_CELL=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				value := strings.Trim(parts[1], `";'`)
				value = strings.TrimSuffix(value, ";")
				os.Setenv("SGE_CELL", value)
			}
		}

		// Handle SGE_CLUSTER_NAME
		if strings.HasPrefix(line, "SGE_CLUSTER_NAME=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				value := strings.Trim(parts[1], `";'`)
				value = strings.TrimSuffix(value, ";")
				os.Setenv("SGE_CLUSTER_NAME", value)
			}
		}

		// Handle SGE_QMASTER_PORT
		if strings.HasPrefix(line, "SGE_QMASTER_PORT=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				value := strings.Trim(parts[1], `";'`)
				value = strings.TrimSuffix(value, ";")
				os.Setenv("SGE_QMASTER_PORT", value)
			}
		}

		// Handle SGE_EXECD_PORT
		if strings.HasPrefix(line, "SGE_EXECD_PORT=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				value := strings.Trim(parts[1], `";'`)
				value = strings.TrimSuffix(value, ";")
				os.Setenv("SGE_EXECD_PORT", value)
			}
		}

		// Handle SGE_ARCH (requires executing $SGE_ROOT/util/arch)
		if strings.Contains(line, "SGE_ARCH=") && strings.Contains(line, "$SGE_ROOT/util/arch") {
			archCmd := exec.Command(filepath.Join(sgeRoot, "util", "arch"))
			archOutput, err := archCmd.Output()
			if err == nil {
				sgeArch := strings.TrimSpace(string(archOutput))
				os.Setenv("SGE_ARCH", sgeArch)
			}
		}

		// Handle DRMAA_LIBRARY_PATH
		if strings.HasPrefix(line, "DRMAA_LIBRARY_PATH=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				value := strings.Trim(parts[1], `";'`)
				value = strings.TrimSuffix(value, ";")
				// Expand $SGE_ROOT if present
				value = strings.ReplaceAll(value, "$SGE_ROOT", sgeRoot)
				os.Setenv("DRMAA_LIBRARY_PATH", value)
			}
		}

		// Handle PATH updates
		if strings.HasPrefix(line, "PATH=") && strings.Contains(line, "$SGE_ROOT") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				value := strings.Trim(parts[1], `";'`)
				value = strings.TrimSuffix(value, ";")
				// Expand $SGE_ROOT and $SGE_ARCH
				value = strings.ReplaceAll(value, "$SGE_ROOT", sgeRoot)
				if sgeArch := os.Getenv("SGE_ARCH"); sgeArch != "" {
					value = strings.ReplaceAll(value, "$SGE_ARCH", sgeArch)
				}
				// Prepend to existing PATH
				currentPath := os.Getenv("PATH")
				if currentPath != "" {
					os.Setenv("PATH", value+":"+currentPath)
				} else {
					os.Setenv("PATH", value)
				}
			}
		}
	}

	return nil
}

// detectAndSetSGERoot automatically detects and sets SGE environment variables
// by loading settings.sh file, similar to "source settings.sh" in shell
// If settingsPath is provided (from config), it will be used first
func detectAndSetSGERoot(settingsPath string) error {
	// If settingsPath is provided (from config), try to use it first
	if settingsPath != "" {
		if _, err := os.Stat(settingsPath); err == nil {
			if err := loadSettingsSh(settingsPath); err == nil {
				log.Printf("Loaded SGE environment variables from configured path: %s", settingsPath)
				return nil
			} else {
				log.Printf("Warning: Failed to load settings.sh from configured path %s: %v, trying auto-detection", settingsPath, err)
			}
		} else {
			log.Printf("Warning: Configured settings.sh path does not exist: %s, trying auto-detection", settingsPath)
		}
	}
	// Check if SGE_ROOT is already set and valid
	if sgeRoot := os.Getenv("SGE_ROOT"); sgeRoot != "" {
		// Verify the path exists
		if _, err := os.Stat(sgeRoot); err == nil {
			// Check if other SGE variables are also set (indicating settings.sh was already sourced)
			if os.Getenv("SGE_CELL") != "" || os.Getenv("SGE_CLUSTER_NAME") != "" {
				// Looks like settings.sh was already sourced, we're good
				return nil
			}
			// SGE_ROOT is set but other vars are not, try to load settings.sh
			settingsPath := filepath.Join(sgeRoot, "default", "common", "settings.sh")
			if _, err := os.Stat(settingsPath); err == nil {
				if err := loadSettingsSh(settingsPath); err == nil {
					log.Printf("Loaded SGE environment variables from settings.sh (SGE_ROOT was already set)")
					return nil
				}
			}
		}
		// If set but path doesn't exist, try to detect a valid path
	}

	// Common SGE installation paths to check
	commonPaths := []string{
		"/opt/gridengine",
		"/usr/share/gridengine",
		"/opt/sge",
		"/usr/local/sge",
		"/opt/sge6",
		"/usr/share/sge",
	}

	// Try to find SGE installation by checking for common/settings.sh
	for _, path := range commonPaths {
		settingsPath := filepath.Join(path, "default", "common", "settings.sh")
		if _, err := os.Stat(settingsPath); err == nil {
			// Found valid SGE installation, load settings.sh
			if err := loadSettingsSh(settingsPath); err == nil {
				log.Printf("Auto-detected and loaded SGE environment from: %s", settingsPath)
				return nil
			}
		}
		// Also check without "default" subdirectory (some installations)
		settingsPathAlt := filepath.Join(path, "common", "settings.sh")
		if _, err := os.Stat(settingsPathAlt); err == nil {
			if err := loadSettingsSh(settingsPathAlt); err == nil {
				log.Printf("Auto-detected and loaded SGE environment from: %s", settingsPathAlt)
				return nil
			}
		}
	}

	// If not found, try to detect from DRMAA library path (if available)
	// Check if we can find libdrmaa.so and infer SGE_ROOT from its path
	// This is a fallback method
	if drmaaLibPath := os.Getenv("LD_LIBRARY_PATH"); drmaaLibPath != "" {
		paths := strings.Split(drmaaLibPath, ":")
		for _, libPath := range paths {
			// Try to find parent directory that looks like SGE root
			// e.g., /opt/gridengine/lib/lx-amd64 -> /opt/gridengine
			if strings.Contains(libPath, "gridengine") || strings.Contains(libPath, "sge") {
				// Go up directories to find potential SGE_ROOT
				current := libPath
				for i := 0; i < 3; i++ {
					parent := filepath.Dir(current)
					settingsPath := filepath.Join(parent, "default", "common", "settings.sh")
					if _, err := os.Stat(settingsPath); err == nil {
						if err := loadSettingsSh(settingsPath); err == nil {
							log.Printf("Auto-detected and loaded SGE environment from library path: %s", settingsPath)
							return nil
						}
					}
					current = parent
				}
			}
		}
	}

	return fmt.Errorf("SGE_ROOT not set and could not auto-detect. Please set SGE_ROOT environment variable, source settings.sh, or ensure SGE is installed in a standard location")
}

// getDRMAASession returns a global DRMAA session (thread-safe)
func getDRMAASession() (*drmaa.Session, error) {
	drmaaSessionMutex.Lock()
	defer drmaaSessionMutex.Unlock()

	if !drmaaSessionInit {
		// Use configured settings path if available, otherwise auto-detect
		settingsPath := configuredSettingsPath
		// Auto-detect and set SGE_ROOT if not already set
		if err := detectAndSetSGERoot(settingsPath); err != nil {
			return nil, fmt.Errorf("failed to set SGE_ROOT: %v", err)
		}

		session, err := drmaa.MakeSession()
		if err != nil {
			return nil, fmt.Errorf("failed to create DRMAA session: %v (SGE_ROOT=%s)", err, os.Getenv("SGE_ROOT"))
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

// formatMemoryGB formats memory value in GB for SGE resource specification
// If the value is an integer, uses %dG format; otherwise uses %.2fG format
func formatMemoryGB(mem float64) string {
	// Check if value is effectively an integer (within floating point precision)
	if mem == math.Trunc(mem) {
		return fmt.Sprintf("%dG", int(mem))
	}
	// Use 2 decimal places for fractional values
	return fmt.Sprintf("%.2fG", mem)
}

func IlterCommand(ctx context.Context, dbObj *MySql, thred int, need2run []int, mode JobMode, cpu int, mem, h_vmem float64, userSetMem, userSetHvmem bool, queue string, sgeProject string, parallelEnvMode string, write_pool *gpool.Pool, hostname string) {
	pool := gpool.New(thred)

	for _, N := range need2run {
		pool.Add(1)
		if mode == ModeQsubSge {
			go SubmitQsubCommand(ctx, N, pool, dbObj, write_pool, cpu, mem, h_vmem, userSetMem, userSetHvmem, queue, sgeProject, parallelEnvMode, hostname)
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

func SubmitQsubCommand(ctx context.Context, N int, pool *gpool.Pool, dbObj *MySql, write_pool *gpool.Pool, cpu int, mem, h_vmem float64, userSetMem, userSetHvmem bool, queue string, sgeProject string, parallelEnvMode string, hostname string) {
	defer pool.Done()

	var subShellPath string
	var retry int
	var currentMem float64
	var currentHvmem float64
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

	// Ensure subShellPath is absolute (required for SGE to correctly determine working directory)
	absSubShellPath, err := filepath.Abs(subShellPath)
	if err != nil {
		write_pool.Add(1)
		now = time.Now().Format("2006-01-02 15:04:05")
		retry++
		pathErr := err // Save original error before database update
		_, dbErr := dbObj.Db.Exec("UPDATE job set status=?, endtime=?, exitCode=?, retry=? where subJob_num=?", J_failed, now, 1, retry, N)
		write_pool.Done()
		if dbErr != nil {
			log.Printf("Error updating database: %v", dbErr)
		}
		if pathErr != nil {
			log.Printf("Error getting absolute path for script: %v", pathErr)
		}
		return
	}
	subShellPath = absSubShellPath

	// Get base name of subShellPath
	subShellBase := filepath.Base(subShellPath)

	// Set job template properties
	// Use absolute script path to ensure SGE can find the script
	// Following goqsub's approach: don't set output paths explicitly, let SGE auto-generate them
	jt.SetRemoteCommand(subShellPath)
	// Set job name to script base name (with .sh extension), matching goqsub's implementation
	// SGE will auto-generate output files as: {job_name}.o.{jobID} and {job_name}.e.{jobID}
	// For example: task_0001.sh.o.8944790 and task_0001.sh.e.8944790
	// Output files will be generated in the script's directory (via -cwd in nativeSpec)
	jt.SetJobName(subShellBase)
	// Note: We don't call SetOutputPath/SetErrorPath - let SGE auto-generate based on job name
	// This matches goqsub's implementation and avoids DRMAA path format issues

	// Build nativeSpec with SGE resource options
	// Following goqsub's pattern: include -cwd to ensure output files are generated in script's directory
	// - SetRemoteCommand sets the script path
	// - -cwd ensures working directory is script's directory
	// - -b n means non-binary mode (use shell)
	// - SGE will auto-generate output files as {job_name}.o.{jobID} and {job_name}.e.{jobID}
	// Note: Following goqsub's pattern:
	// - --mem maps to -l vf=XG (virtual free memory)
	// - --h_vmem maps to -l h_vmem=XG (hard virtual memory limit)
	// Two parallel environment modes:
	// - pe_smp mode: -pe smp Y -cwd -b n (Y=cpu)
	// - num_proc mode (default): -l p=Y -cwd -b n (p=cpu)

	// Build resource specification
	var resourceSpecs []string

	// Add memory specifications if set
	if userSetMem {
		resourceSpecs = append(resourceSpecs, fmt.Sprintf("vf=%s", formatMemoryGB(mem)))
	}
	if userSetHvmem {
		resourceSpecs = append(resourceSpecs, fmt.Sprintf("h_vmem=%s", formatMemoryGB(h_vmem)))
	}

	// Add hostname specification if provided (non-empty and not "none")
	// Supports single hostname or comma-separated list (e.g., node1 or node1,node2)
	if hostname != "" && strings.ToLower(strings.TrimSpace(hostname)) != "none" {
		hostnameValue := strings.TrimSpace(hostname)
		resourceSpecs = append(resourceSpecs, fmt.Sprintf("h=%s", hostnameValue))
	}

	// Build nativeSpec
	var nativeSpecParts []string

	// Add parallel environment or CPU specification based on mode
	if parallelEnvMode == string(ParallelEnvPeSmp) {
		// pe_smp mode: use -pe smp (matches goqsub)
		nativeSpecParts = append(nativeSpecParts, fmt.Sprintf("-pe smp %d", cpu))
	} else {
		// num_proc mode: add p=cpu to -l specification
		resourceSpecs = append(resourceSpecs, fmt.Sprintf("p=%d", cpu))
	}

	// Add -cwd to use current working directory (where qsub was executed) as job's working directory
	// -cwd is a boolean flag in SGE and does not accept a path argument
	// -b n means non-binary mode (use shell)
	nativeSpecParts = append(nativeSpecParts, "-cwd", "-b n")

	// Add resource specifications if any
	if len(resourceSpecs) > 0 {
		nativeSpecParts = append(nativeSpecParts, fmt.Sprintf("-l %s", strings.Join(resourceSpecs, ",")))
	}

	// Add queue specification if provided (supports multiple queues, comma-separated)
	if queue != "" {
		// Trim whitespace from queue string, but preserve internal structure
		// Only trim leading/trailing whitespace, not commas (commas are valid separators)
		queue = strings.TrimSpace(queue)
		// Only remove trailing commas (if user accidentally added them)
		queue = strings.TrimRight(queue, ",")
		// Trim any remaining whitespace after removing trailing commas
		queue = strings.TrimSpace(queue)
		if queue != "" {
			nativeSpecParts = append(nativeSpecParts, fmt.Sprintf("-q %s", queue))
		}
	}

	// Add SGE project specification if provided (for resource quota management)
	if sgeProject != "" {
		nativeSpecParts = append(nativeSpecParts, fmt.Sprintf("-P %s", sgeProject))
	}

	nativeSpec := strings.Join(nativeSpecParts, " ")
	jt.SetNativeSpecification(nativeSpec)

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
			// Log nativeSpec for debugging queue issues
			log.Printf("Error submitting job: %v (nativeSpec: %s)", submitErr, nativeSpec)
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

			// Check error file for memory-related errors
			// SGE generates output files in different formats depending on version:
			// - Format 1: {job_name}.o.{jobID} and {job_name}.e.{jobID} (with dot separator)
			// - Format 2: {job_name}.o{jobID} and {job_name}.e{jobID} (without dot separator)
			// Try both formats to support different SGE versions
			// Note: job_name includes .sh extension (e.g., task_0001.sh)
			errFile := filepath.Join(subShellDir, fmt.Sprintf("%s.e.%s", subShellBase, jobID))
			if _, err := os.Stat(errFile); os.IsNotExist(err) {
				// Try format without dot separator (some SGE versions use this)
				errFileAlt := filepath.Join(subShellDir, fmt.Sprintf("%s.e%s", subShellBase, jobID))
				if _, err := os.Stat(errFileAlt); err == nil {
					errFile = errFileAlt
				}
			}
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
						newMem = math.Ceil(mem * 1.25)
					}
					if userSetHvmem {
						newHvmem = math.Ceil(h_vmem * 1.25)
					}
				}
				// Store as float64 in database (database will handle conversion if needed)
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
