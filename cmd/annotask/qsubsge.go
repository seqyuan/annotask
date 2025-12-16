package main

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/akamensky/argparse"
)

// parseMemoryString parses memory string and converts it to GB (float64)
// Supports formats: "2", "2G", "2g", "200m", "200M"
// Returns the value in GB as float64
func parseMemoryString(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty memory string")
	}

	// Remove all whitespace (including spaces between number and unit)
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")

	// Regular expression to match number and optional unit
	re := regexp.MustCompile(`^(\d+(?:\.\d+)?)([GgMm])?$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid memory format: %s (expected format: number with optional G/g/M/m suffix, e.g., 2, 2G, 200m)", s)
	}

	// Parse the number
	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number in memory string: %s", s)
	}

	// Get unit (if any)
	unit := strings.ToUpper(matches[2])
	if unit == "" {
		// No unit specified, assume GB
		return value, nil
	}

	// Convert to GB based on unit
	switch unit {
	case "G":
		return value, nil
	case "M":
		// Convert MB to GB
		return value / 1000.0, nil
	default:
		return 0, fmt.Errorf("unsupported memory unit: %s (supported: G, g, M, m)", unit)
	}
}

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
	opt_t := parser.Int("t", "thread", &argparse.Options{Default: 10, Help: "Max concurrent tasks to run (default: 10)"})
	opt_project := parser.String("", "project", &argparse.Options{Default: config.Project, Help: fmt.Sprintf("Project name (default: %s)", config.Project)})
	opt_cpu := parser.Int("", "cpu", &argparse.Options{Default: config.Defaults.CPU, Help: fmt.Sprintf("Number of CPUs per task (default: %d)", config.Defaults.CPU)})
	opt_mem := parser.String("", "mem", &argparse.Options{Required: false, Help: "Virtual memory (vf) per task (maps to -l vf=XG, only used if explicitly set). Supports formats: 2, 2G, 2g, 200m, 200M"})
	opt_h_vmem := parser.String("", "h_vmem", &argparse.Options{Required: false, Help: "Hard virtual memory limit (h_vmem) per task (maps to -l h_vmem=XG, only used if explicitly set). Supports formats: 2, 2G, 2g, 200m, 200M"})
	opt_queue := parser.String("", "queue", &argparse.Options{Default: config.Queue, Help: fmt.Sprintf("Queue name(s), comma-separated for multiple queues (default: %s)", config.Queue)})
	// Format help message for sge-project
	sgeProjectHelp := "SGE project name for resource quota management"
	if config.SgeProject != "" {
		sgeProjectHelp = fmt.Sprintf("%s (default: %s)", sgeProjectHelp, config.SgeProject)
	} else {
		sgeProjectHelp = fmt.Sprintf("%s (default: from config, or empty if not set)", sgeProjectHelp)
	}
	opt_sge_project := parser.String("P", "sge-project", &argparse.Options{Default: config.SgeProject, Help: sgeProjectHelp})
	opt_mode := parser.String("", "mode", &argparse.Options{Default: "pe_smp", Help: "Parallel environment mode: pe_smp (use -pe smp X, default) or num_proc (use -l p=X)"})

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

	// Parse mem and h_vmem values
	var mem float64
	var h_vmem float64
	var errMem, errHvmem error

	if userSetMem && opt_mem != nil && *opt_mem != "" {
		mem, errMem = parseMemoryString(*opt_mem)
		if errMem != nil {
			log.Fatalf("Error parsing --mem value: %v", errMem)
		}
	}
	if userSetHvmem && opt_h_vmem != nil && *opt_h_vmem != "" {
		h_vmem, errHvmem = parseMemoryString(*opt_h_vmem)
		if errHvmem != nil {
			log.Fatalf("Error parsing --h_vmem value: %v", errHvmem)
		}
	}

	// Note: We don't auto-calculate h_vmem from mem anymore.
	// Only use values that user explicitly set via --mem or --h_vmem flags.

	// Get queue value
	// If user explicitly set --queue, use it; otherwise use config.Queue (from user home or executable dir)
	queue := config.Queue // Default from config (user home config takes precedence)
	if opt_queue != nil && *opt_queue != "" {
		queue = *opt_queue // Command line argument takes highest precedence
	}

	// Get SGE project value
	// If user explicitly set -P/--sge-project, use it; otherwise use config.SgeProject
	sgeProject := config.SgeProject // Default from config (user home config takes precedence)
	if opt_sge_project != nil && *opt_sge_project != "" {
		sgeProject = *opt_sge_project // Command line argument takes highest precedence
	}

	// Get mode value and validate
	mode := "pe_smp" // default
	if opt_mode != nil && *opt_mode != "" {
		mode = *opt_mode
	}

	// Validate mode value
	if mode != "pe_smp" && mode != "num_proc" {
		log.Fatalf("Invalid --mode value: %s. Must be 'pe_smp' or 'num_proc'", mode)
	}

	runTasks(config, *opt_i, *opt_l, *opt_t, *opt_project, ModeQsubSge, *opt_cpu, mem, h_vmem, userSetMem, userSetHvmem, queue, sgeProject, mode)

	// Close DRMAA session when qsubsge mode completes
	closeDRMAASession()
}
