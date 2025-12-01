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

	err := parser.Parse(args)
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

	// If h_vmem is not set (0), calculate as mem * 1.25
	h_vmem := *opt_h_vmem
	if h_vmem == 0 {
		h_vmem = int(float64(*opt_mem) * 1.25)
	}

	runTasks(config, *opt_i, *opt_l, *opt_p, *opt_project, ModeQsubSge, *opt_cpu, *opt_mem, h_vmem)
}

