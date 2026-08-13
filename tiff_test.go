package main

import (
	"encoding/binary"
	"image"
	"testing"
)

// tinyTIFF builds the smallest valid uncompressed little-endian TIFF that
// matches what pdfcpu emits: pixels first, then the IFD.
func tinyTIFF(w, h, spp, photometric int, pix []byte) []byte {
	le := binary.LittleEndian
	pixOff := 8
	ifdOff := pixOff + len(pix)
	buf := make([]byte, ifdOff)
	copy(buf[0:], "II")
	le.PutUint16(buf[2:], 42)
	le.PutUint32(buf[4:], uint32(ifdOff))
	copy(buf[pixOff:], pix)

	tags := [][3]int{
		{256, 3, w}, {257, 3, h}, {258, 3, 8}, {259, 3, 1},
		{262, 3, photometric}, {273, 4, pixOff}, {277, 3, spp},
		{278, 3, h}, {279, 4, len(pix)},
	}
	entry := make([]byte, 12)
	hdr := make([]byte, 2)
	le.PutUint16(hdr, uint16(len(tags)))
	buf = append(buf, hdr...)
	for _, t := range tags {
		le.PutUint16(entry[0:], uint16(t[0]))
		le.PutUint16(entry[2:], uint16(t[1]))
		le.PutUint32(entry[4:], 1)
		le.PutUint32(entry[8:], uint32(t[2]))
		buf = append(buf, entry...)
	}
	buf = append(buf, 0, 0, 0, 0) // next IFD = 0
	return buf
}

func TestBareTIFFGray(t *testing.T) {
	pix := []byte{10, 20, 30, 40} // 2x2 grayscale
	img, err := bareTIFF(tinyTIFF(2, 2, 1, 1, pix))
	if err != nil {
		t.Fatalf("gray: %v", err)
	}
	if img.Bounds() != image.Rect(0, 0, 2, 2) {
		t.Fatalf("gray bounds %v", img.Bounds())
	}
	g, ok := img.(*image.Gray)
	if !ok || g.Pix[0] != 10 || g.Pix[3] != 40 {
		t.Fatalf("gray pixels wrong: %v", img)
	}
}

func TestBareTIFFCMYK(t *testing.T) {
	// 1x1 CMYK: pure black via K, which is what a grayscale drawing becomes.
	pix := []byte{0, 0, 0, 255}
	img, err := bareTIFF(tinyTIFF(1, 1, 4, 5, pix))
	if err != nil {
		t.Fatalf("cmyk: %v", err)
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	if r>>8 > 5 || g>>8 > 5 || b>>8 > 5 {
		t.Fatalf("cmyk K=255 should be near black, got %d %d %d", r>>8, g>>8, b>>8)
	}
}

// A compressed or non-8-bit TIFF must be refused, not guessed at.
func TestBareTIFFRefusesCompressed(t *testing.T) {
	b := tinyTIFF(2, 2, 1, 1, []byte{1, 2, 3, 4})
	// flip Compression (tag 259) value from 1 to 5 (LZW). IFD entries start
	// after the 2-byte entry count at the IFD offset in the header.
	le := binary.LittleEndian
	for i := int(le.Uint32(b[4:])) + 2; i+12 <= len(b); i += 12 {
		if le.Uint16(b[i:]) == 259 {
			le.PutUint32(b[i+8:], 5)
		}
	}
	if _, err := bareTIFF(b); err == nil {
		t.Fatal("expected refusal of compressed TIFF")
	}
}
