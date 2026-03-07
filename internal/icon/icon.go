package icon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

// ICO returns the app icon as an ICO file (PNG-in-ICO, Windows Vista+ format).
// Suitable for systray.SetIcon.
func ICO() []byte {
	return encodeICO([]image.Image{drawAt(32)})
}

// Images returns the icon at multiple sizes for embedding in Windows resources.
func Images() []image.Image {
	return []image.Image{drawAt(256), drawAt(48), drawAt(32), drawAt(16)}
}

// drawAt renders the icon at the given pixel size.
// Design: bright blue upward arrow on a transparent background.
func drawAt(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	arrow := color.NRGBA{79, 186, 255, 255} // #4FBAFF electric blue

	s := float64(size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			nx := (float64(x) + 0.5) / s
			ny := (float64(y) + 0.5) / s
			if inArrow(nx, ny) {
				img.SetNRGBA(x, y, arrow)
			}
		}
	}
	return img
}

// inArrow returns true if the normalised point (px, py) lies within the
// upward-pointing arrow shape. Coordinates are in the [0,1]×[0,1] unit square.
//
//	Head: triangle, tip (0.50, 0.08) → base from (0.12, 0.54) to (0.88, 0.54)
//	Body: centred rectangle x:[0.35, 0.65]  y:[0.50, 0.92]
func inArrow(px, py float64) bool {
	if py >= 0.08 && py <= 0.54 {
		hw := (py - 0.08) / (0.54 - 0.08) * 0.38
		if math.Abs(px-0.5) <= hw {
			return true
		}
	}
	return px >= 0.35 && px <= 0.65 && py >= 0.50 && py <= 0.92
}

// encodeICO packs images into a PNG-in-ICO container.
// Each image is stored as a raw PNG stream; Windows Vista+ handles this natively.
func encodeICO(imgs []image.Image) []byte {
	type chunk struct {
		data []byte
		w, h int
	}
	chunks := make([]chunk, len(imgs))
	for i, img := range imgs {
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		b := img.Bounds()
		chunks[i] = chunk{data: buf.Bytes(), w: b.Dx(), h: b.Dy()}
	}

	var out bytes.Buffer
	// ICONDIR (6 bytes)
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))           // reserved
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))           // type: icon
	_ = binary.Write(&out, binary.LittleEndian, uint16(len(chunks))) // image count

	// ICONDIRENTRY × n (16 bytes each); image data follows the header block.
	offset := 6 + 16*len(chunks)
	for _, c := range chunks {
		w, h := c.w, c.h
		if w >= 256 {
			w = 0 // ICO encodes 256 as 0
		}
		if h >= 256 {
			h = 0
		}
		out.WriteByte(byte(w))
		out.WriteByte(byte(h))
		out.WriteByte(0) // colour count (0 = no palette)
		out.WriteByte(0) // reserved
		_ = binary.Write(&out, binary.LittleEndian, uint16(1))              // planes
		_ = binary.Write(&out, binary.LittleEndian, uint16(32))             // bit depth
		_ = binary.Write(&out, binary.LittleEndian, uint32(len(c.data)))    // byte size
		_ = binary.Write(&out, binary.LittleEndian, uint32(offset))         // data offset
		offset += len(c.data)
	}
	for _, c := range chunks {
		out.Write(c.data)
	}
	return out.Bytes()
}
