package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

type song struct {
	path, title, artist, version, code, state string
}

func decodeText(data []byte) string {
	if utf8.Valid(data) {
		return strings.TrimPrefix(string(data), "\ufeff")
	}
	decoded, _ := charmap.Windows874.NewDecoder().Bytes(data)
	return string(decoded)
}

func settings(data []byte) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.FieldsFunc(decodeText(data), func(r rune) bool { return r == '\r' || r == '\n' }) {
		name, value, ok := strings.Cut(line, "=")
		if ok {
			values[strings.ToUpper(strings.TrimSpace(name))] = strings.TrimSpace(strings.Trim(value, "\x00"))
		}
	}
	return values
}

func songInfo(path string) (song, error) {
	entry := song{path: path, title: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), version: "ไม่ระบุ"}
	file, err := os.Open(path)
	if err != nil {
		return entry, err
	}
	defer file.Close()
	parts, err := readParts(file, true)
	if err != nil {
		return entry, err
	}
	info := settings(parts["SONG_INFO"])
	entry.code = info["CODE"]
	if version := settings(parts["HEADER"])["VERSION"]; version != "" {
		entry.version = version
	}
	if string(parts["FORMAT"]) == "XSTM" {
		if entry.version == "ไม่ระบุ" {
			entry.version = "XSTM"
		} else {
			entry.version += " (XSTM)"
		}
	}
	entry.artist = info["ARTIST"]
	lyrics := strings.Split(strings.ReplaceAll(decodeText(parts[".lyr"]), "\r\n", "\n"), "\n")
	if title := info["TITLE"]; title != "" {
		entry.title = title
	} else if len(lyrics) >= 3 && strings.TrimSpace(lyrics[0]) != "" {
		entry.title = strings.TrimSpace(lyrics[0])
	}
	if entry.artist == "" && len(lyrics) >= 3 {
		entry.artist = strings.TrimSpace(lyrics[1])
	}
	return entry, nil
}

func scanSongs(folder string, visit func(string), failed func(string, error)) {
	filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			failed(path, err)
			return nil
		}
		if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(path), ".emk") {
			visit(path)
		}
		return nil
	})
}

// Each search term can match a different field.
func matchingSongs(rows []song, query string) []int {
	terms := strings.Fields(strings.ToLower(query))
	matches := make([]int, 0, len(rows))
	for i, entry := range rows {
		if matchesSong(entry, terms) {
			matches = append(matches, i)
		}
	}
	return matches
}

func matchesSong(entry song, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	fields := strings.ToLower(strings.Join([]string{
		entry.title, entry.artist, entry.version, "EMK v" + entry.version, entry.code,
	}, "\n"))
	for _, term := range terms {
		if !strings.Contains(fields, term) {
			return false
		}
	}
	return true
}
