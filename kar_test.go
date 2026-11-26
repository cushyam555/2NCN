package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestKARToNCN(t *testing.T) {
	track := []byte{
		0, 0xff, 1, 7, '@', 'T', 'T', 'i', 't', 'l', 'e',
		0, 0xff, 1, 8, '@', 'T', 'A', 'r', 't', 'i', 's', 't',
		0, 0x90, 60, 64,
		0, 61, 64, // Running status.
		96, 0xff, 1, 4, '/', 'O', 'n', 'e',
		96, 0xff, 1, 4, '\\', 'T', 'w', 'o',
		0, 0xff, 0x2f, 0,
	}
	midi := []byte("MThd\x00\x00\x00\x06\x00\x00\x00\x01\x00\x60MTrk")
	midi = binary.BigEndian.AppendUint32(midi, uint32(len(track)))
	midi = append(midi, track...)
	parts, err := unpack(bytes.NewReader(midi))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(parts[".mid"], midi) {
		t.Fatal("MIDI bytes changed")
	}
	if got := string(parts[".lyr"]); got != "Title\r\nArtist\r\n\r\n\r\nOne\r\nTwo\r\n" {
		t.Errorf("lyrics = %q", got)
	}
	expected := []byte{24, 0, 24, 0, 24, 0, 24, 0, 48, 0, 48, 0, 48, 0, 48, 0}
	if got := parts[".cur"]; !bytes.Equal(got, expected) {
		t.Errorf("cursor = %v, want %v", got, expected)
	}
	if _, err := unpack(bytes.NewReader(midi[:len(midi)-1])); err == nil {
		t.Fatal("accepted a truncated track")
	}
}

func TestKARLyricEvents(t *testing.T) {
	track, err := readKARTrack([]byte{
		0, 0xff, 5, 3, 'a', 'b', 'c',
		100, 0xff, 5, 2, '\n', 'd',
		0, 0xff, 0x2f, 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	lyrics, cursor, err := buildKARLyrics(track.lyrics, 100, "")
	if err != nil || string(lyrics) != "abc\r\nd\r\n" || len(cursor) != 12 ||
		binary.LittleEndian.Uint16(cursor[8:10]) != 24 {
		t.Fatalf("lyrics %q, cursor %v, error %v", lyrics, cursor, err)
	}
}
