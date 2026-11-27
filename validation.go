package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

func validateNCNParts(parts map[string][]byte) error {
	if err := validateMIDI(parts[".mid"]); err != nil {
		return err
	}

	if !hasNCNLyrics(parts[".lyr"]) {
		return errors.New("ไม่มีเนื้อร้องในข้อมูล .lyr")
	}
	if len(parts[".cur"]) == 0 {
		return errors.New("ไม่มีจังหวะเนื้อร้องในข้อมูล .cur")
	}
	return nil
}

func hasNCNLyrics(data []byte) bool {
	// NCN lyrics start with four metadata lines before the sung text.
	lines := strings.SplitN(strings.ReplaceAll(decodeText(data), "\r\n", "\n"), "\n", 5)
	return len(lines) == 5 && strings.Trim(lines[4], " \t\r\n\x00\x1a") != ""
}

func validateMIDI(data []byte) error {
	if len(data) < 14 || string(data[:4]) != "MThd" {
		return errors.New("หัวไฟล์ MIDI ไม่สมบูรณ์")
	}
	headerSize := binary.BigEndian.Uint32(data[4:8])
	if headerSize < 6 || uint64(headerSize) > uint64(len(data)-8) {
		return errors.New("ขนาดหัวไฟล์ MIDI ไม่ถูกต้อง")
	}
	format := binary.BigEndian.Uint16(data[8:10])
	tracks := int(binary.BigEndian.Uint16(data[10:12]))
	division := binary.BigEndian.Uint16(data[12:14])
	if format > 2 || tracks == 0 || (format == 0 && tracks != 1) || division == 0 {
		return errors.New("ค่ารูปแบบ จำนวนแทร็ก หรือหน่วยจังหวะ MIDI ไม่ถูกต้อง")
	}
	if division&0x8000 != 0 {
		frames := int8(division >> 8)
		if (frames != -24 && frames != -25 && frames != -29 && frames != -30) || byte(division) == 0 {
			return errors.New("หน่วยจังหวะ SMPTE ของ MIDI ไม่ถูกต้อง")
		}
	}

	remaining := data[8+int(headerSize):]
	for track := 1; track <= tracks; {
		if len(remaining) < 8 {
			// Some format-1 files declare one extra track. Only correct the
			// header when all physical tracks passed validation and the last
			// track ends exactly at EOF.
			if len(remaining) == 0 && format == 1 && track == tracks && track > 1 {
				binary.BigEndian.PutUint16(data[10:12], uint16(track-1))
				return nil
			}
			return fmt.Errorf("ข้อมูล MIDI ขาดก่อนแทร็ก %d จาก %d", track, tracks)
		}
		size := binary.BigEndian.Uint32(remaining[4:8])
		if uint64(size) > uint64(len(remaining)-8) {
			return fmt.Errorf("ข้อมูล MIDI ถูกตัดขาดก่อนจบแทร็ก %d", track)
		}
		end := 8 + int(size)
		// Standard MIDI permits readers to skip unrecognized chunk types.
		if string(remaining[:4]) == "MTrk" {
			body := remaining[8:end]
			if !bytes.HasSuffix(body, []byte{0xff, 0x2f, 0}) {
				return fmt.Errorf("MIDI แทร็ก %d ไม่มีเครื่องหมายจบแทร็ก", track)
			}
			if err := walkMIDITrack(body, nil, true); err != nil {
				return fmt.Errorf("MIDI แทร็ก %d: %w", track, err)
			}
			track++
		}
		remaining = remaining[end:]
	}
	return nil
}
