package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExportCodes(t *testing.T) {
	rows := []song{{path: "other.emk", code: "12345"}, {path: "B0001.emk"}, {path: "third.emk", code: "00009"}}
	chosen := []int{2, 0, 1}
	for _, test := range []struct {
		start string
		want  []string
	}{
		{"", []string{"00009", "12345", "B0001"}},
		{"A0099", []string{"A0099", "A0100", "A0101"}},
		{"099", []string{"099", "100", "101"}},
	} {
		got, err := exportCodes(rows, chosen, test.start)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Errorf("%q: got %v, %v, want %v", test.start, got, err, test.want)
		}
	}
	for _, bad := range []string{"ABC", "../0001", "18446744073709551615"} {
		if _, err := exportCodes(rows, chosen, bad); err == nil {
			t.Errorf("accepted invalid start %q", bad)
		}
	}
}

func TestWriteNCNSetProtectsExistingFiles(t *testing.T) {
	folder := t.TempDir()
	parts := map[string][]byte{".mid": {1}, ".lyr": {2}, ".cur": {3}}
	existing := filepath.Join(folder, "001.lyr")
	if err := os.WriteFile(existing, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeNCNSet(folder, "001", parts); err == nil {
		t.Fatal("accepted existing component")
	}
	if got, err := os.ReadFile(existing); err != nil || string(got) != "existing" {
		t.Fatalf("existing file altered: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(folder, "001.mid")); !os.IsNotExist(err) {
		t.Fatalf("incomplete triplet left behind: %v", err)
	}
	if err := writeNCNSet(folder, "002", parts); err != nil {
		t.Fatal(err)
	}
	for ext, want := range parts {
		got, err := os.ReadFile(filepath.Join(folder, "002"+ext))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: got %v, %v", ext, got, err)
		}
	}
}
