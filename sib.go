package main

import (
	"archive/zip"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

const sibSignature = "SIBAES"

// These are the fixed format parameters of the supplied NCN-to-SIB converter.
const sibKey = "Sibsib@KARAOKE_Pro#2026!Global$$"
const sibIV = "Sib#2026!InitVec"

func readSIB(data []byte, metadataOnly bool) (map[string][]byte, error) {
	payload := data[len(sibSignature):]
	if len(payload) == 0 || len(payload)%aes.BlockSize != 0 {
		return nil, errors.New("ขนาดข้อมูล SIBAES ไม่ถูกต้อง")
	}
	block, err := aes.NewCipher([]byte(sibKey))
	if err != nil {
		return nil, err
	}
	cipher.NewCBCDecrypter(block, []byte(sibIV)).CryptBlocks(payload, payload)
	padding := int(payload[len(payload)-1])
	invalidPadding := errors.New("ข้อมูล SIBAES เสียหายหรือใช้รูปแบบการเข้ารหัสอื่น")
	if padding == 0 || padding > aes.BlockSize {
		return nil, invalidPadding
	}
	for _, value := range payload[len(payload)-padding:] {
		if value != byte(padding) {
			return nil, invalidPadding
		}
	}
	payload = payload[:len(payload)-padding]
	archive, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return nil, fmt.Errorf("เปิดข้อมูล SIB: %w", err)
	}

	entries := make(map[string]*zip.File, 3)
	for _, entry := range archive.File {
		ext := strings.ToLower(path.Ext(entry.Name))
		if entry.FileInfo().IsDir() || (ext != ".mid" && ext != ".lyr" && ext != ".cur") {
			continue
		}
		if entries[ext] != nil || entry.UncompressedSize64 > maxSize {
			return nil, fmt.Errorf("รายการ SIB %s ซ้ำหรือมีขนาดเกินกำหนด", ext)
		}
		entries[ext] = entry
	}
	if len(entries) != 3 {
		return nil, errors.New("ไฟล์ SIB มีข้อมูล MIDI เนื้อร้อง หรือจังหวะไม่ครบ")
	}

	parts := make(map[string][]byte, 3)
	for ext, entry := range entries {
		if metadataOnly && ext != ".lyr" {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("เปิดรายการ SIB %s: %w", ext, err)
		}
		content, err := io.ReadAll(io.LimitReader(reader, maxSize+1))
		reader.Close()
		if err != nil || uint64(len(content)) != entry.UncompressedSize64 {
			return nil, fmt.Errorf("ข้อมูล SIB %s เสียหายหรือขนาดไม่ตรง", ext)
		}
		parts[ext] = content
	}
	if metadataOnly {
		parts["FORMAT"] = []byte("SIB")
		name := path.Base(entries[".mid"].Name)
		code := strings.TrimSuffix(name, path.Ext(name))
		if validSongCode(code) {
			parts["SONG_INFO"] = []byte("CODE=" + code)
		}
	} else if err := restoreMIDIHeader(parts[".mid"]); err != nil {
		return nil, err
	}
	return parts, nil
}
