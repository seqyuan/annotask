package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/akamensky/argparse"
	"github.com/seqyuan/annotask/pkg/gpool"
)

// runLocalMode runs tasks in local mode
func runLocalMode(config *Config, args []string) {
	// Check for help flag before parsing
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			printModuleHelp("local", config)
			return
		}
	}

	parser := argparse.NewParser("annotask local", "Run tasks locally")
	opt_i := parser.String("i", "infile", &argparse.Options{Required: true, Help: "Input shell command file (one command per line or grouped by -l)"})
	opt_l := parser.Int("l", "line", &argparse.Options{Default: config.Defaults.Line, Help: fmt.Sprintf("Number of lines to group as one task (default: %d)", config.Defaults.Line)})
	opt_p := parser.Int("p", "thread", &argparse.Options{Default: config.Defaults.Thread, Help: fmt.Sprintf("Max concurrent tasks to run (default: %d)", config.Defaults.Thread)})
	opt_project := parser.String("", "project", &argparse.Options{Default: config.Project, Help: fmt.Sprintf("Project name (default: %s)", config.Project)})

	// Prepend program name for argparse.Parse (it expects os.Args-like format)
	parseArgs := append([]string{"annotask"}, args...)
	err := parser.Parse(parseArgs)
	if err != nil {
		// If help is requested, show module help instead of just parser usage
		errStr := err.Error()
		if strings.Contains(strings.ToLower(errStr), "help") {
			printModuleHelp("local", config)
			return
		}
		fmt.Print(parser.Usage(err))
		return
	}

	// For local mode, mem and h_vmem are not used, but we still need to pass values
	// Use placeholder values (won't be used in local mode)
	mem := 1
	h_vmem := 1
	// Local mode doesn't use DRMAA, so mem/h_vmem/queue/sge-project/mode flags are not relevant
	runTasks(config, *opt_i, *opt_l, *opt_p, *opt_project, ModeLocal, config.Defaults.CPU, mem, h_vmem, false, false, "", "", "pe_smp")
}

// runTasks is the common function to run tasks in both modes
func runTasks(config *Config, infile string, line, thread int, project string, mode JobMode, cpu, mem, h_vmem int, userSetMem, userSetHvmem bool, queue string, sgeProject string, parallelEnvMode string) {

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
	go MonitorTaskStatus(ctx, dbObj, globalDB, usrID, project, module, string(mode), shellAbsPath, startTime, config, &wg)

	// Create write_pool at runTasks level to ensure it outlives all goroutines
	// This prevents WaitGroup reuse issues when monitoring loops continue after IlterCommand returns
	write_pool := gpool.New(1)

	// Retry loop for failed tasks
	maxRetries := config.Retry.Max
	for retryCount := 0; retryCount < maxRetries; retryCount++ {
		IlterCommand(ctx, dbObj, thread, need2run, mode, cpu, mem, h_vmem, userSetMem, userSetHvmem, queue, sgeProject, parallelEnvMode, write_pool)
		need2run = GetNeed2Run(dbObj)
		if len(need2run) == 0 {
			break
		}
		time.Sleep(2 * time.Second)
	}

	// Wait for all database write operations to complete
	// This must be done before stopping the monitor goroutine
	write_pool.Wait()

	// Stop the monitor goroutine
	cancel()
	wg.Wait()

	// Final update to global DB
	endTime := time.Now()
	total, pending, failed, running, finished, _ := GetTaskStats(dbObj)
	node := GetNodeName(string(mode), config, dbObj)
	UpdateGlobalTaskRecord(globalDB, usrID, project, module, string(mode), shellAbsPath, startTime, total, pending, failed, running, finished, node)
	// Update endtime
	startTimeStr := startTime.Format("2006-01-02 15:04:05")
	endTimeStr := endTime.Format("2006-01-02 15:04:05")
	_, err = globalDB.Db.Exec("UPDATE tasks SET endtime=? WHERE usrID=? AND project=? AND module=? AND starttime=?",
		endTimeStr, usrID, project, module, startTimeStr)
	if err != nil {
		log.Printf("Warning: Could not update endtime: %v", err)
	}

	// Update module status based on final task results
	// Check if there are any failed tasks (exitCode != 0)
	var failedCount int
	err = dbObj.Db.QueryRow("SELECT COUNT(*) FROM job WHERE exitCode!=0").Scan(&failedCount)
	if err != nil {
		log.Printf("Warning: Could not check failed tasks count: %v", err)
	} else {
		if failedCount > 0 {
			if updateErr := UpdateGlobalTaskStatus(globalDB, usrID, project, module, startTime, "failed"); updateErr != nil {
				log.Printf("Warning: Could not update module status to failed: %v", updateErr)
			}
		} else {
			if updateErr := UpdateGlobalTaskStatus(globalDB, usrID, project, module, startTime, "completed"); updateErr != nil {
				log.Printf("Warning: Could not update module status to completed: %v", updateErr)
			}
		}
	}

	CheckExitCode(dbObj)
}
