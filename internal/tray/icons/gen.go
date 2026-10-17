//go:build ignore

// gen draws the tray's four padlock icons and writes them as multi-size ICO
// files holding PNG images (supported since Windows Vista).
//
//	cd internal/tray/icons && go run gen.go
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

type style struct {
	name  string
	fill  color.NRGBA
	open  bool // shackle swung open
	slash bool // agent unreachable
}

func main() {
	styles := []style{
		{name: "unreachable", fill: color.NRGBA{0x8a, 0x8a, 0x8a, 0xff}, slash: true},
		{name: "keys", fill: color.NRGBA{0x3b, 0x82, 0xf6, 0xff}, open: true},
		{name: "locked", fill: color.NRGBA{0xe0, 0x8a, 0x1e, 0xff}},
		{name: "unlocked", fill: color.NRGBA{0x22, 0xa5, 0x5a, 0xff}, open: true},
	}
	for _, s := range styles {
		var pngs [][]byte
		for _, size := range []int{16, 32, 48} {
			var buf bytes.Buffer
			if err := png.Encode(&buf, draw(s, size)); err != nil {
				log.Fatal(err)
			}
			pngs = append(pngs, buf.Bytes())
		}
		if err := os.WriteFile(s.name+".ico", ico(pngs, []int{16, 32, 48}), 0o644); err != nil {
			log.Fatal(err)
		}
	}
}

// draw renders one padlock at size px, supersampled 4x per axis.
func draw(s style, size int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 4
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			hits := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					u := (float64(x) + (float64(sx)+0.5)/ss) / float64(size)
					v := (float64(y) + (float64(sy)+0.5)/ss) / float64(size)
					if inside(s, u, v) {
						hits++
					}
				}
			}
			if hits > 0 {
				c := s.fill
				c.A = uint8(int(c.A) * hits / (ss * ss))
				img.SetNRGBA(x, y, c)
			}
		}
	}
	return img
}

// inside reports whether unit-square point (u, v) is part of the padlock.
func inside(s style, u, v float64) bool {
	// Body: a rounded rectangle across the lower half.
	if roundRect(u, v, 0.14, 0.44, 0.86, 0.96, 0.1) {
		// Keyhole cut out of the body.
		if math.Hypot(u-0.5, v-0.64) < 0.07 || (math.Abs(u-0.5) < 0.03 && v > 0.64 && v < 0.8) {
			return false
		}
		return true
	}
	// Shackle: a ring arc above the body; the open one rises and shifts left.
	cx, cy := 0.5, 0.42
	if s.open {
		cx, cy = 0.34, 0.3
	}
	r := math.Hypot(u-cx, v-cy)
	if r > 0.2 && r < 0.3 && v < cy {
		return true
	}
	// Shackle legs down into the body.
	legBottom := 0.46
	if math.Abs(u-(cx-0.25)) < 0.05 && v >= cy && v < legBottom {
		return true
	}
	if !s.open && math.Abs(u-(cx+0.25)) < 0.05 && v >= cy && v < legBottom {
		return true
	}
	if s.slash && math.Abs((u-v)/math.Sqrt2) < 0.05 {
		return true
	}
	return false
}

func roundRect(u, v, x0, y0, x1, y1, r float64) bool {
	if u < x0 || u > x1 || v < y0 || v > y1 {
		return false
	}
	cx := math.Max(x0+r, math.Min(u, x1-r))
	cy := math.Max(y0+r, math.Min(v, y1-r))
	return math.Hypot(u-cx, v-cy) <= r
}

// ico wraps PNG images in an ICO container.
func ico(pngs [][]byte, sizes []int) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, [3]uint16{0, 1, uint16(len(pngs))})
	offset := 6 + 16*len(pngs)
	for i, p := range pngs {
		dim := uint8(sizes[i])
		b.Write([]byte{dim, dim, 0, 0})
		binary.Write(&b, binary.LittleEndian, [2]uint16{1, 32}) // planes, bits per pixel
		binary.Write(&b, binary.LittleEndian, [2]uint32{uint32(len(p)), uint32(offset)})
		offset += len(p)
	}
	for _, p := range pngs {
		b.Write(p)
	}
	return b.Bytes()
}
