package gles

import (
	"testing"
)

func TestSnapshotReadsRGBAAndFlipsRows(t *testing.T) {
	const width, height = 2, 2
	// glReadPixels returns the bottom row first.
	bottomUp := []byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
	}
	got := snapshotImage(bottomUp, width, height)
	want := []byte{
		9, 10, 11, 12, 13, 14, 15, 16,
		1, 2, 3, 4, 5, 6, 7, 8,
	}
	for i := range want {
		if got.Pix[i] != want[i] {
			t.Fatalf("pixel byte %d = %d, want %d", i, got.Pix[i], want[i])
		}
	}
}
