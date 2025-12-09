package main

import (
	"fmt"
	"log"
	"os"
)

// printModuleList prints list of available modules
func printModuleList() {
	fmt.Println("annotask - parallel task v1.7.11")
	fmt.Println()
	fmt.Println("Available modules:")
	fmt.Println("    local             Run tasks locally (default module)")
	fmt.Println("    qsubsge           Submit tasks to qsub SGE system")
	fmt.Println("    stat              Query task status from global database")
	fmt.Println("    delete            Delete task records from global database")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("    annotask                    Show this help")
	fmt.Println("    annotask <module>           Run a module")
	fmt.Println("    annotask <module> --help    Show module-specific help")
	fmt.Println("    annotask -i <file>          Run local module (default)")
}

// printModuleHelp prints help for a specific module
func printModuleHelp(module string, config *Config) {
	switch module {
	case "local":
		fmt.Println("annotask local - Run tasks locally")
		fmt.Println()
		fmt.Println("USAGE:")
		fmt.Println("    annotask local -i|--infile <file> [OPTIONS]")
		fmt.Println("    annotask -i|--infile <file> [OPTIONS]  (local is default)")
		fmt.Println()
		fmt.Println("OPTIONS:")
		fmt.Println("    -h, --help        Print help information")
		fmt.Println("    -i, --infile      Input shell command file (one command per line or grouped by -l) (required)")
		fmt.Println("    -l, --line        Number of lines to group as one task (default: 1)")
		fmt.Println("    -p, --thread      Max concurrent tasks to run (default: 1)")
		fmt.Println("    --project         Project name (default: default)")
	case "qsubsge":
		fmt.Println("annotask qsubsge - Submit tasks to qsub SGE system")
		fmt.Println()
		fmt.Println("USAGE:")
		fmt.Println("    annotask qsubsge -i|--infile <file> [OPTIONS]")
		fmt.Println()
		fmt.Println("OPTIONS:")
		fmt.Println("    -h, --help        Print help information")
		fmt.Println("    -i, --infile      Input shell command file (required)")
		fmt.Println("    -l, --line        Number of lines to group as one task (default: 1)")
		fmt.Println("    -p, --thread      Max concurrent tasks to run (default: 1)")
		fmt.Println("    --project         Project name (default: default)")
		fmt.Println("    --cpu             Number of CPUs per task (default: 1)")
		fmt.Println("    --mem             Memory in GB per task (default: 1)")
		fmt.Println("    --h_vmem          Virtual memory in GB per task (default: mem * 1.25)")
		fmt.Println("    --queue            Queue name(s), comma-separated for multiple queues (default: from config)")
		fmt.Println("    -P, --sge-project  SGE project name for resource quota management (default: from config)")
	case "stat":
		fmt.Println("annotask stat - Query task status from global database")
		fmt.Println()
		fmt.Println("USAGE:")
		fmt.Println("    annotask stat [-p|--project <project>]")
		fmt.Println()
		fmt.Println("OPTIONS:")
		fmt.Println("    -h, --help        Print help information")
		fmt.Println("    -p, --project     Filter by project name")
	case "delete":
		fmt.Println("annotask delete - Delete task records from global database")
		fmt.Println()
		fmt.Println("USAGE:")
		fmt.Println("    annotask delete -p|--project <project> [-m|--module <module>]")
		fmt.Println()
		fmt.Println("OPTIONS:")
		fmt.Println("    -h, --help        Print help information")
		fmt.Println("    -p, --project     Project name (required)")
		fmt.Println("    -m, --module      Module (shell path basename without extension)")
	default:
		fmt.Printf("Unknown module: %s\n", module)
		fmt.Println()
		printModuleList()
	}
}

// isModuleName checks if the argument is a module name
func isModuleName(arg string) bool {
	modules := []string{"local", "qsubsge", "stat", "delete"}
	for _, m := range modules {
		if arg == m {
			return true
		}
	}
	return false
}

// isOption checks if the argument looks like an option (starts with -)
func isOption(arg string) bool {
	return len(arg) > 0 && arg[0] == '-'
}

func main() {
	// Load configuration
	config, err := LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// If no arguments, show module list
	if len(os.Args) == 1 {
		printModuleList()
		return
	}

	// Check if first argument is a module name
	if len(os.Args) > 1 {
		firstArg := os.Args[1]

		// Check for help on module list
		if firstArg == "--help" || firstArg == "-h" {
			printModuleList()
			return
		}

		// Check if it's a module name
		if isModuleName(firstArg) {
			// Check if help is requested for this module
			if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
				printModuleHelp(firstArg, config)
				return
			}

			// Run the module
			switch firstArg {
			case "local":
				runLocalMode(config, os.Args[2:])
				return
			case "stat":
				RunStatModule(config, os.Args[2:])
				return
			case "delete":
				RunDeleteModule(config, os.Args[2:])
				return
			case "qsubsge":
				// QsubSge mode as subcommand
				runQsubSgeMode(config, os.Args[2:])
				return
			}
		}

		// If first argument is an option (like -i), default to local module
		if isOption(firstArg) {
			runLocalMode(config, os.Args[1:])
			return
		}

		// Unknown argument
		fmt.Printf("Unknown module or option: %s\n", firstArg)
		fmt.Println()
		printModuleList()
		os.Exit(1)
	}
}
