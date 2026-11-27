package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const xstmHeaderSize = 12

var xstmKey = [...]byte{0x2c, 0x33, 0x79, 0x92, 0x16, 0x75, 0x83, 0x43}

func readXSTM(data []byte, metadataOnly bool) (map[string][]byte, error) {
	if version := binary.LittleEndian.Uint32(data[4:8]); version != 1 {
		return nil, fmt.Errorf("ยังไม่รองรับหัว XSTM รุ่น %d", version)
	}
	// The little-endian length describes the section between the header and the song blocks.
	prefixSize := int(binary.LittleEndian.Uint16(data[10:12]))
	if prefixSize >= len(data)-xstmHeaderSize {
		return nil, errors.New("ส่วนหัว XSTM ถูกตัดขาด")
	}
	blocks := data[xstmHeaderSize+prefixSize:]
	if len(blocks) < 2 {
		return nil, errors.New("บล็อก XSTM ถูกตัดขาด")
	}

	// Some XSTM writers use the same block key as SFDS.
	var blockKey [8]byte
	for _, candidate := range [...][8]byte{xstmKey, key} {
		method, flags := blocks[0]^candidate[0], blocks[1]^candidate[1]
		if method&0x0f == 8 && method>>4 <= 7 && (uint16(method)<<8|uint16(flags))%31 == 0 {
			blockKey = candidate
			break
		}
	}
	if blockKey == ([8]byte{}) {
		return nil, errors.New("หัวบล็อก XSTM ไม่ตรงกับรูปแบบที่รองรับ")
	}
	for i := range blocks {
		blocks[i] ^= blockKey[i%len(blockKey)]
	}
	reader := bytes.NewReader(blocks)
	parts := make(map[string][]byte, 4)
	for _, name := range [...]string{"HEADER", "SONG_INFO", ".mid", ".lyr", ".cur"} {
		isMetadata := name == "HEADER" || name == "SONG_INFO"
		keep := name == ".lyr" || metadataOnly == isMetadata
		payload, err := readXSTMBlock(reader, keep || name == "HEADER")
		if err != nil {
			return nil, fmt.Errorf("อ่าน XSTM %s: %w", name, err)
		}
		if name == "HEADER" && settings(payload)["SIGNATURE"] != "EMK" {
			return nil, errors.New("หัวข้อมูลเพลง XSTM ไม่ถูกต้อง")
		}
		if keep {
			parts[name] = payload
		}
	}
	if reader.Len() != 0 {
		return nil, errors.New("พบข้อมูลเกินท้ายไฟล์ XSTM")
	}
	if metadataOnly {
		parts["FORMAT"] = []byte("XSTM")
	} else if err := restoreMIDIHeader(parts[".mid"]); err != nil {
		return nil, err
	}
	return parts, nil
}

func readXSTMBlock(reader *bytes.Reader, keep bool) ([]byte, error) {
	before := reader.Len()
	compressed, err := zlib.NewReader(reader)
	if err != nil {
		return nil, err
	}

	var payload bytes.Buffer
	target := io.Writer(io.Discard)
	if keep {
		target = &payload
	}
	size, err := io.Copy(target, io.LimitReader(compressed, maxSize+1))
	compressed.Close()
	if err != nil {
		return nil, err
	}
	if size > maxSize {
		return nil, errors.New("ข้อมูลหลังคลายบีบอัดมีขนาดเกินกำหนด")
	}

	// XSTM pads each zlib stream to eight bytes. The last byte records the
	// size of the partial block, with zeroes before it.
	used := before - reader.Len()
	if remainder := used % len(xstmKey); remainder != 0 {
		var trailer [len(xstmKey)]byte
		padding := trailer[:len(xstmKey)-remainder]
		if _, err := io.ReadFull(reader, padding); err != nil {
			return nil, err
		}
		if padding[len(padding)-1] != byte(remainder) || len(bytes.Trim(padding[:len(padding)-1], "\x00")) != 0 {
			return nil, errors.New("ข้อมูลท้ายบล็อก XSTM ไม่ถูกต้อง")
		}
	} else if reader.Len() >= len(xstmKey) {
		var trailer [len(xstmKey)]byte
		reader.ReadAt(trailer[:], reader.Size()-int64(reader.Len()))
		if trailer == [len(xstmKey)]byte{} {
			reader.Seek(int64(len(trailer)), io.SeekCurrent)
		}
	}
	return payload.Bytes(), nil
}
