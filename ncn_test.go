package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanNCNLibrary(t *testing.T) {
	root := t.TempDir()
	midi := "MThd\x00\x00\x00\x06\x00\x00\x00\x01\x00\x60MTrk\x00\x00\x00\x04\x00\xff\x2f\x00"
	for path, content := range map[string]string{
		"Song/A/001.MID":   midi,
		"Lyrics/A/001.LYR": "ชื่อเพลง\r\nศิลปิน\r\nC\r\n\r\nเนื้อร้อง",
		"Cursor/A/001.CUR": "\x00\x00",
		"other/001.mid":    "invalid",
		"other/001.lyr":    "\r\nArtist\r\nC\r\n\r\n",
		"other/002.cur":    "",
	} {
		path = filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	var rows []song
	scanSongs(root, ncnExtension, func(path string) { t.Errorf("unexpected package %s", path) }, func(set ncnSet) {
		entry, _ := inspectNCN(set)
		rows = append(rows, entry)
	}, func(path string, err error) {
		t.Errorf("scan %s: %v", path, err)
	})
	if len(rows) != 3 {
		t.Fatalf("got %d sets, want 3", len(rows))
	}
	if rows[0].title != "ชื่อเพลง" || rows[0].artist != "ศิลปิน" || rows[0].status != successfulSong {
		t.Errorf("unexpected complete set: %+v", rows[0])
	}
	for i, reasons := range [][]string{
		{"หัวไฟล์ MIDI", "ไม่มีชื่อเพลง", "ไม่มีเนื้อร้อง", "ขาดไฟล์ .cur"},
		{"ขาดไฟล์ .mid", "ขาดไฟล์ .lyr", "ไม่มีจังหวะเนื้อร้อง"},
	} {
		entry := rows[i+1]
		for _, reason := range reasons {
			if entry.status != skippedSong || !strings.Contains(entry.state, reason) {
				t.Errorf("expected %q in %+v", reason, entry)
			}
		}
	}
	if got := catalogView(rows, "", skippedSong, ncnExtension, 0, 0); len(got) != 2 {
		t.Fatalf("problem filter returned %v", got)
	}
	if got := convertibleIndices(rows, []int{0, 1, 2}); len(got) != 0 {
		t.Fatalf("NCN sets appeared in convertible rows: %v", got)
	}
}

func TestScanNCNFromLibraryRootAndParent(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "NCN")
	for _, path := range []string{
		"Cursor/0/001.CUR", "Lyrics/0/001.LYR", "Song/0/001.MID",
		"Cursor/Z/Z123.cur", "Lyrics/z/Z123.lyr", "Song/Z/Z123.mid",
		"extra.EMK",
	} {
		path = filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}

	for _, folder := range []string{root, parent} {
		for _, extension := range []string{ncnExtension, "", ".emk"} {
			var packages, sets int
			scanSongs(folder, extension, func(string) {
				packages++
			}, func(set ncnSet) {
				sets++
				for _, path := range set.paths {
					if path == "" {
						t.Errorf("incomplete match for %s from %s", set.code, folder)
					}
				}
			}, func(path string, err error) {
				t.Errorf("scan %s: %v", path, err)
			})
			wantPackages, wantSets := 1, 2
			if extension == ncnExtension {
				wantPackages = 0
			} else if extension == ".emk" {
				wantSets = 0
			}
			if packages != wantPackages || sets != wantSets {
				t.Errorf("scan %s (%q) returned %d packages, %d sets", folder, extension, packages, sets)
			}
		}
	}
}
