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

func TestRestoreMIDIWithTrailersOrAlternateTracks(t *testing.T) {
	original := []byte("MThd\x00\x00\x00\x06\x00\x01\x00\x01\x00\x60MTrk\x00\x00\x00\x04\x00\xff\x2f\x00")
	for _, sig := range []string{"XOa\xa8", "MUf\xad", "JPr\xb2", "MThq", "cofm"} {
		data := append(bytes.Clone(original), 0, 0, 42)
		copy(data, sig)
		if err := restoreMIDIHeader(data); err != nil || !bytes.Equal(data[:len(original)], original) ||
			!bytes.Equal(data[len(original):], []byte{0, 0, 42}) {
			t.Fatalf("trailing data for %q: %x, %v", sig, data, err)
		}
	}
	data := bytes.Clone(original)
	copy(data, "MThq")
	copy(data[14:], "MTrq")
	binary.BigEndian.PutUint16(data[10:12], 2)
	if err := restoreMIDIHeader(data); err != nil || !bytes.Equal(data, original) {
		t.Fatalf("alternate track and miscount: %x, %v", data, err)
	}
}

func TestCompleteSFDSFromTimedMIDILyrics(t *testing.T) {
	track := []byte{24, 0xff, 5, 3, 'H', 'e', 'y', 0, 0xff, 0x2f, 0}
	midi := []byte("MThq\x00\x00\x00\x06\x00\x00\x00\x01\x00\x60MTrk")
	midi = binary.BigEndian.AppendUint32(midi, uint32(len(track)))
	midi = append(midi, track...)
	parts, err := completeSFDS(map[string][]byte{"KAR_DATA": midi})
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 || string(parts[".lyr"]) != "\r\n\r\n\r\n\r\nHey\r\n" ||
		!bytes.Equal(parts[".cur"], []byte{6, 0, 6, 0, 6, 0, 6, 0}) || string(parts[".mid"][:4]) != "MThd" {
		t.Fatalf("unexpected NCN parts: %v", parts)
	}
}
