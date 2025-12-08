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

	// For local mode, h_vmem is not used, but we still need to pass a value
	// Use mem * 1.25 as default (though it won't be used in local mode)
	h_vmem := int(float64(config.Defaults.Mem) * 1.25)
	runTasks(config, *opt_i, *opt_l, *opt_p, *opt_project, ModeLocal, config.Defaults.CPU, config.Defaults.Mem, h_vmem)
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

