package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

const smdHeader = "\x04\x00\x00\x00\x05\x00\x00\x001.num"
const smdClearBytes = 16
const smdKey = "VG7DNMXHVG7DNMXH5CE0EFB6CB28057461E5B906C8941A8F0DD82D0FACAB89FF827323A96658E696F0950F53D48BA0468ED6CAF55FA5416C8FAE813B8E0CC587"

func readSMD(data []byte, metadataOnly bool) (map[string][]byte, error) {
	for i := smdClearBytes; i < len(data); i++ {
		data[i] -= smdKey[(i-smdClearBytes)%len(smdKey)]
	}
	reader := bytes.NewReader(data[4:])
	parts := make(map[string][]byte, 3)
	seen := make(map[string]bool, 4)
	code := ""
	for range 4 {
		var nameSize, size uint32
		if err := binary.Read(reader, binary.LittleEndian, &nameSize); err != nil ||
			nameSize == 0 || nameSize > 260 || uint64(nameSize)+4 > uint64(reader.Len()) {
			return nil, errors.New("ชื่อรายการใน SMD ไม่ถูกต้อง")
		}
		name := make([]byte, nameSize)
		if _, err := io.ReadFull(reader, name); err != nil {
			return nil, err
		}
		if err := binary.Read(reader, binary.LittleEndian, &size); err != nil || size > maxSize {
			return nil, errors.New("ขนาดข้อมูล SMD ไม่ถูกต้อง")
		}
		filename := decodeText(name)
		ext := strings.ToLower(filepath.Ext(filename))
		if seen[ext] || (ext != ".num" && ext != ".mid" && ext != ".lyr" && ext != ".cur") {
			return nil, fmt.Errorf("รายการ SMD ไม่ถูกต้อง (%s)", filename)
		}
		seen[ext] = true
		if ext == ".mid" {
			code = strings.TrimSuffix(filename, filepath.Ext(filename))
		}

		compressed, err := zlib.NewReader(reader)
		if err != nil {
			return nil, fmt.Errorf("เปิดข้อมูล SMD %s: %w", ext, err)
		}
		var payload bytes.Buffer
		target := io.Writer(io.Discard)
		keep := ext == ".lyr" || (!metadataOnly && ext != ".num")
		if keep {
			target = &payload
		}
		written, err := io.Copy(target, io.LimitReader(compressed, int64(size)+1))
		compressed.Close()
		if err != nil {
			return nil, fmt.Errorf("อ่านข้อมูล SMD %s: %w", ext, err)
		}
		if written != int64(size) {
			return nil, fmt.Errorf("ขนาดข้อมูล SMD %s ไม่ตรงกับหัวรายการ", ext)
		}
		if keep {
			parts[ext] = payload.Bytes()
		}
	}
	if reader.Len() != 0 {
		return nil, errors.New("พบข้อมูลเกินท้ายไฟล์ SMD")
	}
	if metadataOnly {
		parts["FORMAT"] = []byte("SMD")
		if validSongCode(code) {
			parts["SONG_INFO"] = []byte("CODE=" + code)
		}
	} else if err := restoreMIDIHeader(parts[".mid"]); err != nil {
		return nil, err
	}
	return parts, nil
}
