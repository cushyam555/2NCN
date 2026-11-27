package main

import (
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type scanIssue struct {
	operation, path, reason string
	songCode, outputCode    string
}

func writeReportCSV(output io.Writer, scanIssues, conversionIssues []scanIssue) error {
	if _, err := io.WriteString(output, "\ufeff"); err != nil {
		return err
	}
	writer := csv.NewWriter(output)
	writer.UseCRLF = true
	if err := writer.Write([]string{"ขั้นตอน", "รายการ", "รหัสเพลง", "รหัสปลายทาง", "ไฟล์ต้นทาง", "สาเหตุ"}); err != nil {
		return err
	}
	for _, group := range []struct {
		stage  string
		issues []scanIssue
	}{{"สแกน", scanIssues}, {"แปลง", conversionIssues}} {
		for _, issue := range group.issues {
			record := []string{group.stage, issue.operation, issue.songCode, issue.outputCode, issue.path, issue.reason}
			for i, field := range record {
				// Treat formula-like values from song metadata as text in Excel.
				trimmed := strings.TrimLeftFunc(field, unicode.IsSpace)
				if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
					record[i] = "'" + field
				}
			}
			if err := writer.Write(record); err != nil {
				return err
			}
		}
	}
	writer.Flush()
	return writer.Error()
}

func saveReportCSV(path string, scanIssues, conversionIssues []scanIssue) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".2ncn-report-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	err = writeReportCSV(file, scanIssues, conversionIssues)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}
