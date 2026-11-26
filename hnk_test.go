package main

import (
	"bytes"
	"testing"
)

func TestExpandHNKBlock(t *testing.T) {
	// One literal, a five-byte overlapping copy, then an extended four-byte copy.
	block := []byte{10, 3, 'A', 18, 16, 0}
	got, err := expandHNKBlock(block)
	if err != nil || !bytes.Equal(got, bytes.Repeat([]byte{'A'}, 10)) {
		t.Fatalf("unexpected output %q: %v", got, err)
	}
	for _, invalid := range [][]byte{block[:len(block)-1], {4, 18}, {2, 7, 'a', 'b', 'c'}, {0, 0}} {
		if _, err := expandHNKBlock(invalid); err == nil {
			t.Errorf("accepted malformed block %v", invalid)
		}
	}
}
