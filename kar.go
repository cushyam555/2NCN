package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

type karCue struct {
	tick uint64
	text []byte
}

type karTrack struct {
	name         string
	lyrics, text []karCue
	titles       [][]byte
	language     string
	karaoke      bool
}

func readKAR(data []byte, metadataOnly bool) (map[string][]byte, error) {
	if len(data) < 14 {
		return nil, errors.New("หัวไฟล์ KAR ถูกตัดขาด")
	}
	size := binary.BigEndian.Uint32(data[4:8])
	if size < 6 || uint64(size) > uint64(len(data)-8) {
		return nil, errors.New("ขนาดหัวไฟล์ KAR ไม่ถูกต้อง")
	}
	format, tracks, division := binary.BigEndian.Uint16(data[8:]), binary.BigEndian.Uint16(data[10:]), binary.BigEndian.Uint16(data[12:])
	if format > 1 || tracks == 0 || division == 0 || division&0x8000 != 0 {
		return nil, errors.New("รองรับ KAR แบบ MIDI 0/1 ที่ใช้จังหวะ PPQN")
	}

	remaining := data[8+int(size):]
	var best karTrack
	var cues []karCue
	var title, artist []byte
	var language string
	bestRank := 0
	for i := range tracks {
		if len(remaining) < 8 || string(remaining[:4]) != "MTrk" {
			return nil, fmt.Errorf("ไม่พบแทร็ก MIDI %d ใน KAR", i+1)
		}
		size := binary.BigEndian.Uint32(remaining[4:8])
		if uint64(size) > uint64(len(remaining)-8) {
			return nil, fmt.Errorf("แทร็ก KAR %d ถูกตัดขาด", i+1)
		}
		track, err := readKARTrack(remaining[8 : 8+int(size)])
		if err != nil {
			return nil, fmt.Errorf("อ่านแทร็ก KAR %d: %w", i+1, err)
		}
		remaining = remaining[8+int(size):]
		if track.language != "" {
			language = track.language
		}
		if len(track.text) == 1 {
			switch track.name {
			case "songtitle", "song title", "title":
				title = track.text[0].text
			case "artist":
				artist = track.text[0].text
			}
		}

		var rank int
		var candidate []karCue
		switch {
		case len(track.lyrics) > 0:
			rank, candidate = 2, track.lyrics
		case track.karaoke || track.name == "words" || track.name == "lyrics" || track.name == "lyric":
			rank, candidate = 1, track.text
		}
		if len(candidate) > 0 && (rank > bestRank || (rank == bestRank && len(candidate) > len(cues))) {
			best, cues, bestRank = track, candidate, rank
		}
	}
	if len(cues) == 0 {
		return nil, errors.New("ไม่พบเนื้อร้องคาราโอเกะในไฟล์ KAR")
	}
	if len(best.titles) > 0 {
		title = best.titles[0]
	}
	if len(best.titles) > 1 {
		artist = best.titles[1]
	}
	if best.language != "" {
		language = best.language
	}
	songTitle := karMetadata(decodeKARText(title, language))
	songArtist := karMetadata(decodeKARText(artist, language))
	if metadataOnly {
		return map[string][]byte{
			"FORMAT":    []byte("KAR"),
			"SONG_INFO": []byte("TITLE=" + songTitle + "\nARTIST=" + songArtist),
		}, nil
	}
	lyrics, cursor, err := buildKARLyrics(cues, division, language)
	if err != nil {
		return nil, err
	}
	header := encodeKARLyrics(songTitle + "\r\n" + songArtist + "\r\n\r\n\r\n")
	return map[string][]byte{".mid": data, ".lyr": append(header, lyrics...), ".cur": cursor}, nil
}

func readKARTrack(data []byte) (karTrack, error) {
	var track karTrack
	err := walkMIDITrack(data, track.addMeta, false)
	return track, err
}

func walkMIDITrack(data []byte, onMeta func(byte, uint64, []byte), strict bool) error {
	var tick uint64
	var status byte
	invalid := errors.New("ข้อมูลเหตุการณ์ MIDI ไม่ครบหรือมีรูปแบบไม่ถูกต้อง")
	for len(data) != 0 {
		delta, n := karVLQ(data)
		if n == 0 || n >= len(data) {
			return invalid
		}
		tick += uint64(delta)
		data = data[n:]
		kind := data[0]
		if kind >= 0x80 {
			data = data[1:]
		} else {
			kind = status
		}
		switch {
		case kind == 0xff || kind == 0xf0 || kind == 0xf7:
			var subtype byte
			if kind == 0xff {
				if len(data) == 0 {
					return invalid
				}
				subtype, data = data[0], data[1:]
			} else {
				status = 0
			}
			length, n := karVLQ(data)
			if n == 0 || int(length) > len(data)-n {
				return invalid
			}
			payload := data[n : n+int(length)]
			data = data[n+int(length):]
			if kind != 0xff {
				continue
			}
			if subtype == 0x2f {
				if strict && (len(payload) != 0 || len(data) != 0) {
					return invalid
				}
				return nil
			}
			if onMeta != nil {
				onMeta(subtype, tick, payload)
			}
		case kind >= 0x80 && kind < 0xf0:
			status = kind
			n = 2
			if kind&0xe0 == 0xc0 {
				n = 1
			}
			if len(data) < n || data[0] >= 0x80 || (n == 2 && data[1] >= 0x80) {
				return invalid
			}
			data = data[n:]
		default:
			return invalid
		}
	}
	return nil
}

func karVLQ(data []byte) (uint32, int) {
	var value uint32
	for i, b := range data[:min(len(data), 4)] {
		value = value<<7 | uint32(b&0x7f)
		if b&0x80 == 0 {
			return value, i + 1
		}
	}
	return 0, 0
}

func (track *karTrack) addMeta(subtype byte, tick uint64, data []byte) {
	if len(data) == 0 {
		return
	}
	if subtype == 0x03 {
		track.name = strings.ToLower(strings.TrimSpace(string(data)))
		return
	}
	if subtype != 0x01 && subtype != 0x05 {
		return
	}
	if len(data) >= 2 && data[0] == '@' {
		switch data[1] {
		case 'T':
			track.karaoke = true
			if len(track.titles) < 2 {
				track.titles = append(track.titles, data[2:])
			}
			return
		case 'L':
			track.karaoke, track.language = true, strings.ToUpper(strings.TrimSpace(string(data[2:])))
			return
		case 'K':
			track.karaoke = true
			return
		case 'I', 'V', 'W':
			return
		}
	}
	cue := karCue{tick, data}
	if subtype == 0x05 {
		track.lyrics = append(track.lyrics, cue)
	} else {
		track.text = append(track.text, cue)
		if data[0] == '/' || data[0] == '\\' {
			track.karaoke = true
		}
	}
}

func karMetadata(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func decodeKARText(data []byte, language string) string {
	if !utf8.Valid(data) && language != "" && !strings.HasPrefix(language, "TH") {
		decoded, _ := charmap.Windows1252.NewDecoder().Bytes(data)
		return string(decoded)
	}
	return decodeText(data)
}

func encodeKARLyrics(text string) []byte {
	encoded := make([]byte, 0, len(text))
	for _, r := range text {
		b, ok := charmap.Windows874.EncodeRune(r)
		if !ok {
			b = '?'
		}
		encoded = append(encoded, b)
	}
	return encoded
}

func buildKARLyrics(cues []karCue, division uint16, language string) ([]byte, []byte, error) {
	var lyrics, cursor []byte
	var line bool
	var boundary uint16
	var haveBoundary bool
	endLine := func(tick uint16) {
		if line {
			lyrics = append(lyrics, '\r', '\n')
			line = false
		}
		boundary, haveBoundary = tick, true
	}
	for _, cue := range cues {
		// NCN stores 24 cursor ticks per quarter note. Round to the closest tick.
		time := (cue.tick*24 + uint64(division)/2) / uint64(division)
		if time > 65535 {
			return nil, nil, errors.New("ตำแหน่งเนื้อร้อง KAR เกินช่วงเวลาที่ NCN รองรับ")
		}
		tick := uint16(time)
		text := strings.TrimRight(decodeKARText(cue.text, language), "\x00")
		if strings.HasPrefix(text, "/") || strings.HasPrefix(text, "\\") {
			text = strings.TrimLeft(text, "/\\")
			endLine(tick)
		}
		text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
		for _, b := range encodeKARLyrics(text) {
			if b == '\n' {
				endLine(tick)
			} else if b >= 0x20 || b == '\t' {
				if !line {
					// One entry marks the beginning of the line, then one per character.
					if !haveBoundary {
						boundary = tick
					}
					cursor = binary.LittleEndian.AppendUint16(cursor, boundary)
					line, haveBoundary = true, false
				}
				lyrics = append(lyrics, b)
				cursor = binary.LittleEndian.AppendUint16(cursor, tick)
			}
		}
	}
	if line {
		lyrics = append(lyrics, '\r', '\n')
	}
	if len(cursor) == 0 || len(bytes.TrimSpace(lyrics)) == 0 {
		return nil, nil, errors.New("ไม่พบข้อความเนื้อร้องในไฟล์ KAR")
	}
	return lyrics, cursor, nil
}
