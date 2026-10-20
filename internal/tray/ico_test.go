package tray

import (
	"bytes"
	"os"
	"testing"
)

func TestIcoImagePicksTheSizeToDraw(t *testing.T) {
	ico, err := os.ReadFile("icons/locked.ico") // 16, 32 and 48 pixels
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ want, got int }{{16, 16}, {20, 32}, {32, 32}, {40, 48}, {64, 48}, {1, 16}} {
		img, w, err := icoImage(ico, tc.want)
		if err != nil {
			t.Fatal(err)
		}
		if w != tc.got {
			t.Errorf("for %dpx: picked %dpx, want %dpx", tc.want, w, tc.got)
		}
		if !bytes.HasPrefix(img, []byte("\x89PNG")) {
			t.Errorf("for %dpx: image is not the PNG the file holds", tc.want)
		}
	}
}

func TestIcoImageRejectsOtherFiles(t *testing.T) {
	for _, b := range [][]byte{nil, []byte("\x89PNG\r\n"), {0, 0, 1, 0, 1, 0}} {
		if _, _, err := icoImage(b, 16); err == nil {
			t.Errorf("%q: want an error", b)
		}
	}
}
