package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSettingsThai(t *testing.T) {
	raw := []byte("TITLE=\xe0\xbe\xc5\xa7\r\nARTIST=A=B\r\nVERSION=2\r\n")
	want := map[string]string{"TITLE": "เพลง", "ARTIST": "A=B", "VERSION": "2"}
	if got := settings(raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("settings = %#v, want %#v", got, want)
	}
}

func TestScanSongsNested(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "อัลบั้ม")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "one.EMK"), filepath.Join(child, "two.emk")}
	for _, path := range append(append([]string(nil), want...), filepath.Join(child, "skip.mid")) {
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	scanSongs(root, func(path string) { got = append(got, path) },
		func(path string, err error) { t.Errorf("scan %s: %v", path, err) })
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}
