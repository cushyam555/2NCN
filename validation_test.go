package main

import (
	"bytes"
	"slices"
	"testing"
)

func TestValidateNCNParts(t *testing.T) {
	midi := []byte("MThd\x00\x00\x00\x06\x00\x00\x00\x01\x00\x60MTrk\x00\x00\x00\x04\x00\xff\x2f\x00")
	lyrics := []byte("Title\r\nArtist\r\nC\r\n\r\nSung text\r\n")
	cursor := []byte{0, 0}
	badEvent := bytes.Clone(midi)
	badEvent[22] = 0x80
	for _, tc := range []struct {
		name                 string
		midi, lyrics, cursor []byte
		valid                bool
	}{
		{"complete", midi, lyrics, cursor, true},
		{"truncated MIDI", midi[:len(midi)-1], lyrics, cursor, false},
		{"invalid MIDI event", badEvent, lyrics, cursor, false},
		{"metadata only", midi, []byte("Title\r\nArtist\r\nC\r\n\r\n"), cursor, false},
		{"missing cursor", midi, lyrics, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNCNParts(map[string][]byte{".mid": tc.midi, ".lyr": tc.lyrics, ".cur": tc.cursor})
			if (err == nil) != tc.valid {
				t.Fatalf("validation returned %v", err)
			}
		})
	}
}

func TestCatalogViewPreservesRowIndices(t *testing.T) {
	rows := []song{
		{title: "Beta", artist: "Band", status: skippedSong},
		{title: "Alpha", artist: "Band", status: skippedSong},
		{title: "Gamma", artist: "Band", status: successfulSong},
		{title: "Delta", artist: "Other", status: skippedSong},
	}
	for order, want := range map[int][]int{1: {1, 0}, -1: {0, 1}} {
		if got := catalogView(rows, "Band", skippedSong, 0, order); !slices.Equal(got, want) {
			t.Fatalf("order %d returned %v, want %v", order, got, want)
		}
	}
}

func TestValidateMIDIRepairsOneExtraDeclaredTrack(t *testing.T) {
	midi := []byte("MThd\x00\x00\x00\x06\x00\x01\x00\x03\x00\x60MTrk\x00\x00\x00\x04\x00\xff\x2f\x00MTrk\x00\x00\x00\x04\x00\xff\x2f\x00")
	for name, invalid := range map[string][]byte{
		"partial next header": append(bytes.Clone(midi), 'M'),
		"partial last track":  midi[:len(midi)-1],
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateMIDI(invalid); err == nil || invalid[11] != 3 {
				t.Fatalf("expected rejection without changing the track count, got %v and count %d", err, invalid[11])
			}
		})
	}
	want := bytes.Clone(midi)
	want[11] = 2
	if err := validateMIDI(midi); err != nil || !bytes.Equal(midi, want) {
		t.Fatalf("expected only the track count to change, got %x and %v", midi, err)
	}
}
