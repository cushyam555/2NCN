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
	want := []string{filepath.Join(root, "one.EMK"), filepath.Join(child, "two.SMD")}
	for _, path := range append(append([]string(nil), want...), filepath.Join(child, "skip.mid")) {
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		extension string
		want      []string
	}{
		{"", want},
		{".emk", want[:1]},
		{".smd", want[1:]},
		{".kar", nil},
	} {
		var got []string
		scanSongs(root, tc.extension, func(path string) { got = append(got, path) },
			func(path string, err error) { t.Errorf("scan %s: %v", path, err) })
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("scan %q = %v, want %v", tc.extension, got, tc.want)
		}
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

func TestCatalogViewSortsByDisplayedColumn(t *testing.T) {
	rows := []song{
		{title: "Alpha", artist: "ขวัญ", version: "SMD", path: "b.emk", state: "แปลงแล้ว"},
		{title: "beta", artist: "กานต์", version: "3.1", path: "A.emk", state: "ข้าม"},
	}
	for column, ascending := range [][]int{{0, 1}, {1, 0}, {1, 0}, {1, 0}, {1, 0}} {
		if got := catalogView(rows, "", allSongs, "", column, 1); !reflect.DeepEqual(got, ascending) {
			t.Errorf("column %d ascending = %v, want %v", column, got, ascending)
		}
		descending := []int{ascending[1], ascending[0]}
		if got := catalogView(rows, "", allSongs, "", column, -1); !reflect.DeepEqual(got, descending) {
			t.Errorf("column %d descending = %v, want %v", column, got, descending)
		}
	}
}

func TestCatalogViewFiltersLoadedFileTypes(t *testing.T) {
	rows := []song{
		{title: "Beta", artist: "Band", path: "beta.EMK", status: skippedSong},
		{title: "Alpha", artist: "Band", path: "alpha.emk", status: skippedSong},
		{title: "Gamma", artist: "Band", path: "gamma.kar", status: skippedSong},
		{title: "Delta", artist: "Band", path: "delta.emk", status: successfulSong},
		{title: "Echo", artist: "Other", path: "echo.emk", status: skippedSong},
	}
	for _, tc := range []struct {
		extension string
		want      []int
	}{
		{".emk", []int{1, 0}},
		{".kar", []int{2}},
		{"", []int{1, 0, 2}},
	} {
		if got := catalogView(rows, "Band", skippedSong, tc.extension, 0, 1); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("filter %q = %v, want %v", tc.extension, got, tc.want)
		}
	}
}
