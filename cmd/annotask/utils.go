package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
)

func CheckCount(rows *sql.Rows) (count int) {
	count = 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		panic(err)
	}
	return count
}

func CheckErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// formatTimeShort formats time string to remove year and seconds
// Input: "2006-01-02 15:04:05" -> Output: "01-02 15:04"
func formatTimeShort(timeStr string) string {
	if timeStr == "" || timeStr == "-" {
		return "-"
	}
	// Parse the time string
	t, err := time.Parse("2006-01-02 15:04:05", timeStr)
	if err != nil {
		// If parsing fails, try to extract manually
		parts := strings.Fields(timeStr)
		if len(parts) >= 2 {
			dateParts := strings.Split(parts[0], "-")
			timeParts := strings.Split(parts[1], ":")
			if len(dateParts) >= 3 && len(timeParts) >= 2 {
				return fmt.Sprintf("%s-%s %s:%s", dateParts[1], dateParts[2], timeParts[0], timeParts[1])
			}
		}
		return timeStr
	}
	// Format without year and seconds
	return t.Format("01-02 15:04")
}

