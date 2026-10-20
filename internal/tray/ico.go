package tray

import (
	"encoding/binary"
	"errors"
)

// icoImage picks the image in an ICO file to draw at size pixels: the
// smallest at least that large, else the largest. It returns that image's
// bytes, a PNG or a BMP without its file header, as CreateIconFromResourceEx
// takes them, and the image's width.
func icoImage(ico []byte, size int) (img []byte, width int, err error) {
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[2:]) != 1 {
		return nil, 0, errors.New("tray: not an icon file")
	}
	count := int(binary.LittleEndian.Uint16(ico[4:]))
	best, bestW := -1, 0
	for i := range count {
		e := 6 + 16*i
		if e+16 > len(ico) {
			return nil, 0, errors.New("tray: icon directory is cut short")
		}
		w := int(ico[e])
		if w == 0 {
			w = 256
		}
		better := best < 0 ||
			(w >= size && (bestW < size || w < bestW)) ||
			(w < size && bestW < size && w > bestW)
		if better {
			best, bestW = i, w
		}
	}
	if best < 0 {
		return nil, 0, errors.New("tray: icon file holds no images")
	}
	e := 6 + 16*best
	n := int(binary.LittleEndian.Uint32(ico[e+8:]))
	off := int(binary.LittleEndian.Uint32(ico[e+12:]))
	if off < 0 || n < 0 || off+n > len(ico) {
		return nil, 0, errors.New("tray: icon image lies outside the file")
	}
	return ico[off : off+n], bestW, nil
}
