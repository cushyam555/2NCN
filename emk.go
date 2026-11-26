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
	if len(data) < 50 || len(data) > maxSize {
		return nil, errors.New("ขนาดไฟล์อยู่นอกช่วงที่รองรับ")
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
		return nil, errors.New("ไม่รู้จักรูปแบบไฟล์ EMK, SMD หรือ HNK")
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
	if len(parts) != 3 {
		return nil, errors.New("ไฟล์ EMK มีข้อมูล MIDI เนื้อร้อง หรือจังหวะไม่ครบ")
	}
	if err := restoreMIDIHeader(parts[".mid"]); err != nil {
		return nil, err
	}
	return parts, nil
}

func restoreMIDIHeader(midi []byte) error {
	if bytes.HasPrefix(midi, []byte("MThd")) {
		return nil
	}
	if len(midi) < 22 || (string(midi[:4]) != "zxio" && string(midi[:4]) != "OOn\xac") ||
		binary.BigEndian.Uint32(midi[4:8]) != 6 {
		return errors.New("ไม่รู้จักหัวไฟล์ MIDI")
	}
	position := 14
	for range int(binary.BigEndian.Uint16(midi[10:12])) {
		if position+8 > len(midi) || string(midi[position:position+4]) != "MTrk" {
			return errors.New("ข้อมูลแทร็ก MIDI ไม่ถูกต้อง")
		}
		size := binary.BigEndian.Uint32(midi[position+4 : position+8])
		if uint64(size) > uint64(len(midi)-position-8) {
			return errors.New("ขนาดข้อมูลแทร็ก MIDI ไม่ถูกต้อง")
		}
		position += 8 + int(size)
	}
	if position != len(midi) {
		return errors.New("ขนาดข้อมูลแทร็ก MIDI ไม่ถูกต้อง")
	}
	copy(midi[:4], "MThd")
	return nil
}

func readSongParts(source string) (map[string][]byte, error) {
	file, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return unpack(file)
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
