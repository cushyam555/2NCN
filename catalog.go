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
	path, title, artist, version, state string
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
	if version := settings(parts["HEADER"])["VERSION"]; version != "" {
		entry.version = version
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
