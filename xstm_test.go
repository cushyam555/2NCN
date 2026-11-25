package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestXSTMConversion(t *testing.T) {
	// Original synthetic song data, so the test needs no karaoke files.
	midi := []byte("MThd\x00\x00\x00\x06\x00\x00\x00\x01\x00\x60MTrk\x00\x00\x00\x04\x00\xff\x2f\x00")
	lyrics := []byte("\xe0\xbe\xc5\xa7\r\nTest artist\r\nC\r\n")
	cursor := []byte{0, 0, 2, 0}
	header := []byte("SIGNATURE=EMK\r\nVERSION=2.0\r\n")
	info := []byte("TITLE=\xe0\xbe\xc5\xa7\r\nARTIST=Test artist\r\n")
	var blocks bytes.Buffer
	for _, chunk := range [][]byte{header, info, midi, lyrics, cursor} {
		start := blocks.Len()
		w := zlib.NewWriter(&blocks)
		if _, err := w.Write(chunk); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if remainder := (blocks.Len() - start) % 8; remainder != 0 {
			blocks.Write(make([]byte, 7-remainder))
			blocks.WriteByte(byte(remainder))
		}
	}

	prefix := []byte("XSTM\x01\x00\x00\x00\x00\x00\x0b\x0012345678901")
	data := append(prefix, blocks.Bytes()...)
	testKey := [8]byte{0x2c, 0x33, 0x79, 0x92, 0x16, 0x75, 0x83, 0x43}
	for i := len(prefix); i < len(data); i++ {
		data[i] ^= testKey[(i-len(prefix))%8]
	}
	root := t.TempDir()
	input := filepath.Join(root, "synthetic.emk")
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	song, err := songInfo(input)
	if err != nil || song.title != "เพลง" || song.artist != "Test artist" || song.version != "2.0 (XSTM)" {
		t.Fatalf("unexpected catalog entry %+v: %v", song, err)
	}
	if err := convert(input, root); err != nil {
		t.Fatal(err)
	}
	for suffix, expected := range map[string][]byte{".mid": midi, ".lyr": lyrics, ".cur": cursor} {
		got, err := os.ReadFile(filepath.Join(root, "synthetic", "synthetic"+suffix))
		if err != nil || !bytes.Equal(got, expected) {
			t.Errorf("unexpected %s output: %v", suffix, err)
		}
	}

	invalidOffset := bytes.Clone(data)
	binary.LittleEndian.PutUint16(invalidOffset[10:12], 0xffff)
	unsupportedVersion := bytes.Clone(data)
	unsupportedVersion[4] = 2
	for _, invalid := range [][]byte{invalidOffset, unsupportedVersion, data[:len(data)-1], append(bytes.Clone(data), 0)} {
		if _, err := unpack(bytes.NewReader(invalid)); err == nil {
			t.Fatal("invalid XSTM accepted")
		}
	}
}
