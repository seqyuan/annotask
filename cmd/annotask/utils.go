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
// Input: "2006-01-02 15:04:05" or "2006-01-02T15:04:05Z" -> Output: "01-02 15:04"
func formatTimeShort(timeStr string) string {
	if timeStr == "" || timeStr == "-" {
		return "-"
	}
	
	// Try different time formats
	formats := []string{
		"2006-01-02 15:04:05",
		time.RFC3339,      // "2006-01-02T15:04:05Z07:00"
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
	}
	
	for _, format := range formats {
		if t, err := time.Parse(format, timeStr); err == nil {
			// Format without year and seconds: MM-DD HH:MM
			return t.Format("01-02 15:04")
		}
	}
	
	// If parsing fails, try to extract manually (fallback)
	// Handle ISO 8601 format: "2006-01-02T15:04:05Z"
	if strings.Contains(timeStr, "T") {
		parts := strings.Split(timeStr, "T")
		if len(parts) >= 2 {
			datePart := parts[0]
			timePart := strings.Split(parts[1], ":")
			if len(timePart) >= 2 {
				dateParts := strings.Split(datePart, "-")
				if len(dateParts) >= 3 {
					return fmt.Sprintf("%s-%s %s:%s", dateParts[1], dateParts[2], timePart[0], timePart[1])
				}
			}
		}
	} else {
		// Handle standard format: "2006-01-02 15:04:05"
		parts := strings.Fields(timeStr)
		if len(parts) >= 2 {
			dateParts := strings.Split(parts[0], "-")
			timeParts := strings.Split(parts[1], ":")
			if len(dateParts) >= 3 && len(timeParts) >= 2 {
				return fmt.Sprintf("%s-%s %s:%s", dateParts[1], dateParts[2], timeParts[0], timeParts[1])
			}
		}
	}
	
	// If all parsing fails, return original string
	return timeStr
}

