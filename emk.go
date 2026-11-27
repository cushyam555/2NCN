package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxSize = 32 << 20

var key = [...]byte{0xaf, 0xf2, 0x4c, 0x9c, 0xe9, 0xea, 0x99, 0x43}

var extensions = map[string]string{
	"MIDI_DATA":   ".mid",
	"KAR_DATA":    "KAR_DATA",
	"LYRIC_DATA":  ".lyr",
	"CURSOR_DATA": ".cur",
	"CUR_DATA":    ".cur",
}

type directory struct {
	*bytes.Reader
	err error
}

func (d *directory) read(n int) []byte {
	data := make([]byte, n)
	if d.err == nil {
		_, d.err = io.ReadFull(d.Reader, data)
	}
	return data
}

func (d *directory) field() any {
	switch kind := d.read(1)[0]; kind {
	case 2:
		return uint32(d.read(1)[0])
	case 3:
		return uint32(binary.LittleEndian.Uint16(d.read(2)))
	case 4:
		return binary.LittleEndian.Uint32(d.read(4))
	case 6:
		return string(d.read(int(d.read(1)[0])))
	default:
		if d.err == nil {
			d.err = fmt.Errorf("ไม่รู้จักฟิลด์ SFDS ชนิด %d", kind)
		}
		return nil
	}
}

func unpack(file io.Reader) (map[string][]byte, error) {
	return readParts(file, false)
}

func readParts(file io.Reader, metadataOnly bool) (map[string][]byte, error) {
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) <= maxSize && bytes.HasPrefix(data, []byte("MThd")) {
		return readKAR(data, metadataOnly)
	}
	if len(data) < 50 || len(data) > maxSize {
		return nil, errors.New("ขนาดไฟล์อยู่นอกช่วงที่รองรับ")
	}
	if bytes.HasPrefix(data, []byte(sibSignature)) {
		return readSIB(data, metadataOnly)
	}
	if bytes.HasPrefix(data, []byte("HNKF")) {
		return readHNK(data, metadataOnly)
	}
	if bytes.HasPrefix(data, []byte(smdHeader)) {
		return readSMD(data, metadataOnly)
	}
	if bytes.HasPrefix(data, []byte("XSTM")) {
		return readXSTM(data, metadataOnly)
	}
	if string(data[:5]) != ".SFDS" {
		for i := range data {
			data[i] ^= key[i%len(key)]
		}
	}
	if string(data[:5]) != ".SFDS" {
		return nil, errors.New("ไม่รู้จักรูปแบบไฟล์ EMK, SMD, HNK, SIB หรือ KAR")
	}
	start := binary.LittleEndian.Uint64(data[0x22:])
	end := binary.LittleEndian.Uint64(data[0x2a:])
	if start < 50 || start >= end || end > uint64(len(data)) {
		return nil, errors.New("ตำแหน่งสารบัญ EMK ไม่ถูกต้อง")
	}

	table := directory{Reader: bytes.NewReader(data[start:end])}
	parts := make(map[string][]byte, 3)
	for table.Len() > 0 {
		if string(table.read(4)) != "SFDS" {
			return nil, errors.New("รายการในสารบัญ SFDS ไม่ถูกต้อง")
		}
		tag, _ := table.field().(string)
		size, sizeOK := table.field().(uint32)
		table.field()
		first, firstOK := table.field().(uint32)
		last, lastOK := table.field().(uint32)
		table.field()
		table.field()
		table.read(16)
		table.field()
		table.field()
		if table.err != nil {
			return nil, fmt.Errorf("อ่านสารบัญ SFDS: %w", table.err)
		}
		extension := extensions[tag]
		if metadataOnly {
			switch tag {
			case "HEADER", "SONG_INFO":
				extension = tag
			case "LYRIC_DATA":
			default:
				continue
			}
		}
		if extension == "" {
			continue
		}
		if _, exists := parts[extension]; exists {
			return nil, fmt.Errorf("พบข้อมูล %s ซ้ำ", extension)
		}
		if !sizeOK || !firstOK || !lastOK || size > maxSize ||
			first < 50 || first >= last || last > uint32(len(data)) {
			return nil, fmt.Errorf("ตำแหน่งหรือขนาดของ %s ไม่ถูกต้อง", extension)
		}
		compressed, err := zlib.NewReader(bytes.NewReader(data[first:last]))
		if err != nil {
			return nil, fmt.Errorf("เปิดข้อมูล %s: %w", extension, err)
		}
		payload, err := io.ReadAll(io.LimitReader(compressed, int64(size)+1))
		compressed.Close()
		if err != nil {
			return nil, fmt.Errorf("อ่านข้อมูล %s: %w", extension, err)
		}
		if len(payload) != int(size) {
			return nil, fmt.Errorf("ขนาดข้อมูล %s ไม่ตรงกับสารบัญ", extension)
		}
		parts[extension] = payload
	}
	if metadataOnly {
		return parts, nil
	}
	return completeSFDS(parts)
}

func completeSFDS(parts map[string][]byte) (map[string][]byte, error) {
	embeddedKAR := parts["KAR_DATA"]
	delete(parts, "KAR_DATA")
	if len(parts[".mid"]) == 0 {
		parts[".mid"] = embeddedKAR
	}
	if len(parts[".mid"]) == 0 {
		return nil, errors.New("ไฟล์ EMK ไม่มีข้อมูล MIDI")
	}
	if err := restoreMIDIHeader(parts[".mid"]); err != nil {
		return nil, err
	}
	haveLyrics := hasNCNLyrics(parts[".lyr"])
	haveCursor := len(parts[".cur"]) != 0
	if haveLyrics && haveCursor {
		return parts, nil
	}

	// Generate lyrics and cursor together from timed MIDI events so the two
	// outputs describe the same text and position for every character.
	recovered, err := readKAR(parts[".mid"], false)
	if err != nil && len(embeddedKAR) != 0 {
		if restoreErr := restoreMIDIHeader(embeddedKAR); restoreErr == nil {
			recovered, err = readKAR(embeddedKAR, false)
		}
	}
	if err != nil {
		missing := ".lyr และ .cur"
		switch {
		case haveLyrics:
			missing = ".cur"
		case haveCursor:
			missing = ".lyr"
		}
		return nil, fmt.Errorf("ไฟล์ EMK ขาด %s และกู้จาก MIDI ไม่ได้: %w", missing, err)
	}
	parts[".lyr"], parts[".cur"] = recovered[".lyr"], recovered[".cur"]
	return parts, nil
}

func restoreMIDIHeader(midi []byte) error {
	if bytes.HasPrefix(midi, []byte("MThd")) {
		return nil
	}
	signature := midi[:min(len(midi), 4)]
	invalidHeader := fmt.Errorf("ไม่รู้จักหัวไฟล์ MIDI (% X)", signature)
	if len(midi) < 22 {
		return invalidHeader
	}
	headerSize := binary.BigEndian.Uint32(midi[4:8])
	if headerSize < 6 || uint64(headerSize) > uint64(len(midi)-8) {
		return invalidHeader
	}
	format := binary.BigEndian.Uint16(midi[8:10])
	tracks := binary.BigEndian.Uint16(midi[10:12])
	division := binary.BigEndian.Uint16(midi[12:14])
	if format > 2 || tracks == 0 || (format == 0 && tracks != 1) || division == 0 {
		return invalidHeader
	}
	if division&0x8000 != 0 {
		frames := int8(division >> 8)
		if (frames != -24 && frames != -25 && frames != -29 && frames != -30) || byte(division) == 0 {
			return invalidHeader
		}
	}

	position := 8 + int(headerSize)
	legacySignature := string(signature) == "zxio" || string(signature) == "OOn\xac"
	var alternateTracks []int
	actual := 0
	for position+8 <= len(midi) {
		name := string(midi[position : position+4])
		standard := name == "MTrk"
		if !standard && actual >= int(tracks) {
			break
		}
		size := binary.BigEndian.Uint32(midi[position+4 : position+8])
		start := position + 8
		if uint64(size) > uint64(len(midi)-start) {
			return fmt.Errorf("แทร็ก MIDI ถูกตัดขาด (หัว % X)", signature)
		}
		end := start + int(size)
		if (!standard || !legacySignature) && !bytes.HasSuffix(midi[start:end], []byte{0xff, 0x2f, 0}) {
			return fmt.Errorf("โครงสร้างแทร็ก MIDI ไม่ถูกต้อง (หัว % X, ตำแหน่ง %d)", signature, position)
		}
		if !standard {
			if _, err := readKARTrack(midi[start:end]); err != nil {
				return fmt.Errorf("อ่านแทร็ก MIDI ที่มีหัว % X ไม่ได้: %w", midi[position:position+4], err)
			}
			alternateTracks = append(alternateTracks, position)
		}
		actual++
		position = end
	}
	if actual == 0 || actual > 65535 || (format == 0 && actual != 1) ||
		(actual < int(tracks) && position != len(midi)) {
		return fmt.Errorf("พบแทร็ก MIDI สมบูรณ์ %d จาก %d แทร็ก (หัว % X)", actual, tracks, signature)
	}
	for _, offset := range alternateTracks {
		copy(midi[offset:offset+4], "MTrk")
	}
	binary.BigEndian.PutUint16(midi[10:12], uint16(actual))
	copy(midi[:4], "MThd")
	return nil
}

func readSongParts(source string) (map[string][]byte, error) {
	file, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	parts, err := unpack(file)
	if err == nil && strings.EqualFold(filepath.Ext(source), ".kar") && bytes.HasPrefix(parts[".lyr"], []byte("\r\n")) {
		name := filepath.Base(source)
		title := strings.TrimSuffix(name, filepath.Ext(name))
		parts[".lyr"] = append(encodeKARLyrics(title), parts[".lyr"]...)
	}
	if err != nil {
		return nil, err
	}
	if err := validateNCNParts(parts); err != nil {
		return nil, err
	}
	return parts, nil
}

func convert(source, destination string) error {
	parts, err := readSongParts(source)
	if err != nil {
		return err
	}

	name := filepath.Base(source)
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if stem == "" || stem == "." || stem == ".." {
		return errors.New("ชื่อไฟล์ต้นทางไม่ถูกต้อง")
	}

	folder := filepath.Join(destination, stem)
	for index := 2; ; index++ {
		err := os.Mkdir(folder, 0755)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		folder = filepath.Join(destination, fmt.Sprintf("%s (%d)", stem, index))
	}
	if err := writeNCNSet(folder, stem, parts); err != nil {
		os.Remove(folder)
		return err
	}
	return nil
}
