package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

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

