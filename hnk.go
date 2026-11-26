package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

func readHNK(data []byte, metadataOnly bool) (map[string][]byte, error) {
	// HNKF is followed by a four-byte tempo and three length-prefixed blocks.
	data = data[8:]
	parts := make(map[string][]byte, 3)
	for _, ext := range [...]string{".mid", ".cur", ".lyr"} {
		if len(data) < 4 || uint64(binary.BigEndian.Uint32(data)) > uint64(len(data)-4) {
			return nil, fmt.Errorf("บล็อก HNK %s ถูกตัดขาด", ext)
		}
		size := int(binary.BigEndian.Uint32(data))
		block := data[4 : 4+size]
		data = data[4+size:]
		if metadataOnly && ext != ".lyr" {
			continue
		}
		decoded, err := expandHNKBlock(block)
		if err != nil {
			return nil, fmt.Errorf("อ่าน HNK %s: %w", ext, err)
		}
		parts[ext] = decoded
	}
	if len(data) != 0 {
		return nil, errors.New("พบข้อมูลเกินท้ายไฟล์ HNK")
	}
	if metadataOnly {
		parts["FORMAT"] = []byte("HNK")
	} else if !bytes.HasPrefix(parts[".mid"], []byte("MThd")) {
		return nil, errors.New("หัวไฟล์ MIDI ใน HNK ไม่ถูกต้อง")
	}
	return parts, nil
}

func expandHNKBlock(block []byte) ([]byte, error) {
	reader := bytes.NewReader(block)
	size, err := binary.ReadUvarint(reader)
	if err != nil || size > maxSize {
		return nil, errors.New("ขนาดข้อมูลหลังคลายบีบอัด HNK ไม่ถูกต้อง")
	}
	decoded := make([]byte, 0, min(int(size), len(block)))
	for len(decoded) < int(size) {
		token, err := binary.ReadUvarint(reader)
		if err != nil {
			return nil, err
		}
		remaining := uint64(int(size) - len(decoded))
		if token&1 != 0 {
			count := token >> 1
			if count == 0 || count > remaining || count > uint64(reader.Len()) {
				return nil, errors.New("ข้อมูลโดยตรงใน HNK ไม่ครบหรือมีขนาดไม่ถูกต้อง")
			}
			start := len(block) - reader.Len()
			decoded = append(decoded, block[start:start+int(count)]...)
			reader.Seek(int64(count), io.SeekCurrent)
			continue
		}

		// Back-references use a three-bit length or an extended varint.
		distance, count := token>>4, (token>>1)&7
		if count == 0 {
			count, err = binary.ReadUvarint(reader)
			if err != nil {
				return nil, err
			}
		}
		if distance == 0 || distance > uint64(len(decoded)) || remaining < 4 || count > remaining-4 {
			return nil, errors.New("ตำแหน่งอ้างอิงในบล็อก HNK ไม่ถูกต้อง")
		}
		start := len(decoded) - int(distance)
		for i := range int(count) + 4 {
			decoded = append(decoded, decoded[start+i])
		}
	}
	if reader.Len() != 0 {
		return nil, errors.New("พบข้อมูลเกินท้ายบล็อก HNK")
	}
	return decoded, nil
}
