package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSaveReportCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "รายงาน.csv")
	if err := os.WriteFile(path, []byte("old report"), 0600); err != nil {
		t.Fatal(err)
	}
	scan := []scanIssue{{operation: "สำรวจเส้นทาง", path: "E:\\เพลง", reason: "อ่านไม่ได้"}}
	conversion := []scanIssue{{
		operation: "ข้ามการแปลง", path: "E:\\เพลง, ทดสอบ.emk",
		reason: "อ่าน \"MIDI\" ไม่ได้\nแทร็ก 1", songCode: "00123", outputCode: "=1+1",
	}}
	if err := saveReportCSV(path, scan, conversion); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body, ok := strings.CutPrefix(string(data), "\ufeff")
	if !ok {
		t.Fatal("missing UTF-8 BOM")
	}
	got, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	want := [][]string{
		{"ขั้นตอน", "รายการ", "รหัสเพลง", "รหัสปลายทาง", "ไฟล์ต้นทาง", "สาเหตุ"},
		{"สแกน", scan[0].operation, "", "", scan[0].path, scan[0].reason},
		{"แปลง", conversion[0].operation, "00123", "'=1+1", conversion[0].path, conversion[0].reason},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, %v", got, err)
	}
}
