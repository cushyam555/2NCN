package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestRestoreMIDIHeaderWithUnknownSignature(t *testing.T) {
	original := []byte("MThd\x00\x00\x00\x06\x00\x00\x00\x01\x00\x60MTrk\x00\x00\x00\x04\x00\xff\x2f\x00")
	changed := bytes.Clone(original)
	copy(changed, []byte{0xdd, 0x12, 0x00, 0x18})
	if err := restoreMIDIHeader(changed); err != nil || !bytes.Equal(changed, original) {
		t.Fatalf("MIDI header was not restored: %x, %v", changed, err)
	}

	for _, tc := range []struct {
		name   string
		change func([]byte) []byte
	}{
		{"missing track", func(data []byte) []byte { binary.BigEndian.PutUint16(data[10:], 0); return data }},
		{"bad division", func(data []byte) []byte { binary.BigEndian.PutUint16(data[12:], 0); return data }},
		{"truncated", func(data []byte) []byte { return data[:len(data)-1] }},
		{"extra payload", func(data []byte) []byte { return append(data, 0) }},
		{"invalid end", func(data []byte) []byte { data[len(data)-2] = 0; return data }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.change(bytes.Clone(original))
			copy(data, "abcd")
			before := bytes.Clone(data)
			if err := restoreMIDIHeader(data); err == nil || !bytes.Equal(data, before) {
				t.Fatalf("malformed MIDI changed or accepted: %x, %v", data, err)
			}
		})
	}
}
