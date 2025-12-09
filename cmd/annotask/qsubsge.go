package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/akamensky/argparse"
)

// runQsubSgeMode runs tasks in qsubsge mode
func runQsubSgeMode(config *Config, args []string) {
	// Check for help flag before parsing
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			printModuleHelp("qsubsge", config)
			return
		}
	}

	// Check node
	if err := CheckNode([]string(config.Node)); err != nil {
		log.Fatalf("Node check failed: %v", err)
	}

	parser := argparse.NewParser("annotask qsubsge", "Submit tasks to qsub SGE system")
	opt_i := parser.String("i", "infile", &argparse.Options{Required: true, Help: "Input shell command file (one command per line or grouped by -l)"})
	opt_l := parser.Int("l", "line", &argparse.Options{Default: config.Defaults.Line, Help: fmt.Sprintf("Number of lines to group as one task (default: %d)", config.Defaults.Line)})
	opt_p := parser.Int("p", "thread", &argparse.Options{Default: config.Defaults.Thread, Help: fmt.Sprintf("Max concurrent tasks to run (default: %d)", config.Defaults.Thread)})
	opt_project := parser.String("", "project", &argparse.Options{Default: config.Project, Help: fmt.Sprintf("Project name (default: %s)", config.Project)})
	opt_cpu := parser.Int("", "cpu", &argparse.Options{Default: config.Defaults.CPU, Help: fmt.Sprintf("Number of CPUs per task (default: %d)", config.Defaults.CPU)})
	opt_mem := parser.Int("", "mem", &argparse.Options{Required: false, Help: "Memory in GB per task (only used if explicitly set)"})
	opt_h_vmem := parser.Int("", "h_vmem", &argparse.Options{Required: false, Help: "Virtual memory in GB per task (default: mem * 1.25 if not set)"})
	opt_queue := parser.String("", "queue", &argparse.Options{Default: config.Queue, Help: fmt.Sprintf("Queue name(s), comma-separated for multiple queues (default: %s)", config.Queue)})
	// Format help message for sge-project
	sgeProjectHelp := "SGE project name for resource quota management"
	if config.SgeProject != "" {
		sgeProjectHelp = fmt.Sprintf("%s (default: %s)", sgeProjectHelp, config.SgeProject)
	} else {
		sgeProjectHelp = fmt.Sprintf("%s (default: from config, or empty if not set)", sgeProjectHelp)
	}
	opt_sge_project := parser.String("P", "sge-project", &argparse.Options{Default: config.SgeProject, Help: sgeProjectHelp})

	// Check if user explicitly set --mem or --h_vmem before parsing
	userSetMem := false
	userSetHvmem := false
	for _, arg := range args {
		if arg == "--mem" {
			userSetMem = true
		}
		if arg == "--h_vmem" {
			userSetHvmem = true
		}
	}

	// Prepend program name for argparse.Parse (it expects os.Args-like format)
	parseArgs := append([]string{"annotask"}, args...)
	err := parser.Parse(parseArgs)
	if err != nil {
		// If help is requested, show module help
		errStr := err.Error()
		if strings.Contains(strings.ToLower(errStr), "help") {
			printModuleHelp("qsubsge", config)
			return
		}
		fmt.Print(parser.Usage(err))
		os.Exit(1)
	}

	// Get mem and h_vmem values
	mem := *opt_mem
	h_vmem := *opt_h_vmem

	// Note: We don't auto-calculate h_vmem from mem anymore.
	// Only use values that user explicitly set via --mem or --h_vmem flags.

	// Get queue value (uses config.Queue as default if not set)
	queue := ""
	if opt_queue != nil {
		queue = *opt_queue
	}

	// Get SGE project value (uses config.SgeProject as default if not set)
	sgeProject := ""
	if opt_sge_project != nil {
		sgeProject = *opt_sge_project
	}

	runTasks(config, *opt_i, *opt_l, *opt_p, *opt_project, ModeQsubSge, *opt_cpu, mem, h_vmem, userSetMem, userSetHvmem, queue, sgeProject)
	
	// Close DRMAA session when qsubsge mode completes
	closeDRMAASession()
}
