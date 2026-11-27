package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const ncnExtension = ".ncn" // Catalog filter for a set of three files.

var ncnExtensions = [...]string{".mid", ".lyr", ".cur"}

type ncnSet struct {
	code       string
	paths      [len(ncnExtensions)]string
	duplicates []string
}

func ncnComponent(path string) int {
	for i, extension := range ncnExtensions {
		if strings.EqualFold(filepath.Ext(path), extension) {
			return i
		}
	}
	return -1
}

func ncnSetKey(relativePath string) string {
	base := strings.TrimSuffix(relativePath, filepath.Ext(relativePath))
	parts := strings.Split(base, string(filepath.Separator))
	// eXtreme Karaoke can store the companion files in parallel trees
	// such as MIDI/A, Lyrics/A and Cursor/A.
	for i := len(parts) - 2; i >= 0; i-- {
		switch strings.ToLower(parts[i]) {
		case "midi", "song", "lyrics", "lyric", "cursor":
			return strings.ToLower(filepath.Join(append(parts[:i], parts[i+1:]...)...))
		}
	}
	return strings.ToLower(base)
}

func readNCNComponent(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSize {
		return nil, errors.New("ขนาดไฟล์อยู่นอกช่วงที่รองรับ")
	}
	return data, nil
}

func inspectNCN(set ncnSet) (song, error) {
	entry := song{code: set.code, title: set.code, version: "NCN", status: successfulSong, state: "ตรวจผ่าน"}
	issues := make([]string, 0, 4)
	for i, extension := range ncnExtensions {
		path := set.paths[i]
		if path == "" {
			issues = append(issues, "ขาดไฟล์ "+extension)
			continue
		}
		if entry.path == "" {
			entry.path = path
		}
		data, err := readNCNComponent(path)
		if err != nil {
			issues = append(issues, fmt.Sprintf("อ่าน %s ไม่ได้: %v", path, err))
			continue
		}
		switch extension {
		case ".mid":
			if err := checkMIDI(data, false); err != nil {
				issues = append(issues, err.Error())
			}
		case ".lyr":
			lines := strings.SplitN(strings.ReplaceAll(decodeText(data), "\r\n", "\n"), "\n", 3)
			if title := strings.Trim(lines[0], " \t\r\n\x00\x1a"); title != "" {
				entry.title = title
			} else {
				issues = append(issues, "ไม่มีชื่อเพลงใน .lyr")
			}
			if len(lines) >= 2 {
				entry.artist = strings.TrimSpace(lines[1])
			}
			if !hasNCNLyrics(data) {
				issues = append(issues, "ไม่มีเนื้อร้องใน .lyr")
			}
		case ".cur":
			if len(data) == 0 {
				issues = append(issues, "ไม่มีจังหวะเนื้อร้องใน .cur")
			}
		}
	}
	for _, path := range set.duplicates {
		issues = append(issues, "พบไฟล์ประกอบซ้ำ "+path)
	}
	if len(issues) != 0 {
		entry.status = skippedSong
		entry.state = strings.Join(issues, " | ")
		return entry, errors.New(entry.state)
	}
	return entry, nil
}

func scanNCNSets(folder string, visit func(ncnSet), failed func(string, error)) {
	sets := make(map[string]*ncnSet)
	var ordered []*ncnSet
	filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			failed(path, err)
			return nil
		}
		component := ncnComponent(path)
		if !entry.Type().IsRegular() || component < 0 {
			return nil
		}
		relative, err := filepath.Rel(folder, path)
		if err != nil {
			failed(path, err)
			return nil
		}
		key := ncnSetKey(relative)
		set := sets[key]
		if set == nil {
			set = &ncnSet{code: sourceFileCode(path)}
			sets[key] = set
			ordered = append(ordered, set)
		}
		if set.paths[component] != "" {
			set.duplicates = append(set.duplicates, path)
		} else {
			set.paths[component] = path
		}
		return nil
	})
	for _, set := range ordered {
		visit(*set)
	}
}
