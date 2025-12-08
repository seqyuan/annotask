package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/akamensky/argparse"
)

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

// RunDeleteModule runs the delete module
func RunDeleteModule(config *Config, args []string) {
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

	// Prepend program name for argparse.Parse (it expects os.Args-like format)
	parseArgs := append([]string{"annotask"}, args...)
	err = deleteParser.Parse(parseArgs)
	if err != nil {
		// If help is requested, show module help
		errStr := err.Error()
		if strings.Contains(strings.ToLower(errStr), "help") {
			printModuleHelp("delete", config)
			return
		}
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
}

