package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

func validSongCode(code string) bool {
	return code != "" && code != "." && code != ".." &&
		!strings.HasSuffix(code, ".") && strings.TrimSpace(code) == code &&
		!strings.ContainsAny(code, `<>:"/\|?*`) &&
		strings.IndexFunc(code, unicode.IsControl) == -1 && filepath.IsLocal(code)
}

func exportCodes(rows []song, indices []int, start string) ([]string, error) {
	codes := make([]string, len(indices))
	start = strings.TrimSpace(start)
	if start == "" || len(indices) == 0 {
		for i, index := range indices {
			song := rows[index]
			codes[i] = song.code
			if codes[i] == "" {
				name := filepath.Base(song.path)
				codes[i] = strings.TrimSuffix(name, filepath.Ext(name))
			}
		}
		return codes, nil
	}

	split := len(start)
	for split > 0 && start[split-1] >= '0' && start[split-1] <= '9' {
		split--
	}
	prefix, suffix := start[:split], start[split:]
	number, err := strconv.ParseUint(suffix, 10, 64)
	if err != nil || !validSongCode(start) || uint64(len(indices)-1) > math.MaxUint64-number {
		return nil, errors.New("รหัสเริ่มต้นต้องลงท้ายด้วยตัวเลข เช่น 0001 หรือ A0001 และอยู่ในช่วงที่รองรับ")
	}
	for i := range codes {
		codes[i] = fmt.Sprintf("%s%0*d", prefix, len(suffix), number+uint64(i))
	}
	return codes, nil
}

func convertFlat(source, destination, code string) error {
	if !validSongCode(code) {
		return fmt.Errorf("รหัสเพลงใช้เป็นชื่อไฟล์ไม่ได้ (%s)", code)
	}
	parts, err := readSongParts(source)
	if err != nil {
		return err
	}
	return writeNCNSet(destination, code, parts)
}

func writeNCNSet(folder, code string, parts map[string][]byte) error {
	var written []string
	success := false
	defer func() {
		if !success {
			for _, path := range written {
				os.Remove(path)
			}
		}
	}()

	// Use one order for all jobs and create files exclusively so an existing
	// triplet, or even a single existing component, is never overwritten.
	for _, ext := range [...]string{".mid", ".lyr", ".cur"} {
		path := filepath.Join(folder, code+ext)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("รหัสซ้ำ: มีไฟล์ %s%s อยู่แล้ว", code, ext)
		}
		if err != nil {
			return err
		}
		written = append(written, path)
		_, err = file.Write(parts[ext])
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
	success = true
	return nil
}
