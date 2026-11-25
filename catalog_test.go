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

func TestMatchingSongs(t *testing.T) {
	rows := []song{
		{title: "คนแบกรัก", artist: "แจ็ค ธนพล", version: "2"},
		{title: "Move On", artist: "Artist", version: "3.1"},
		{title: "อีกครั้ง", artist: "ARTIST", version: "ไม่ระบุ"},
	}
	tests := []struct {
		query string
		want  []int
	}{
		{"", []int{0, 1, 2}},
		{"แบกรัก แจ็ค v2", []int{0}},
		{"  ARTIST  ", []int{1, 2}},
		{"move 3.1", []int{1}},
		{"ไม่ระบุ", []int{2}},
		{"ไม่มีเพลงนี้", []int{}},
	}
	for _, test := range tests {
		if got := matchingSongs(rows, test.query); !reflect.DeepEqual(got, test.want) {
			t.Errorf("matchingSongs(%q) = %v, want %v", test.query, got, test.want)
		}
	}
}
