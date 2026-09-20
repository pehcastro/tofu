//go:build windows

package sys

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"image/png"
	"testing"
)

func twoByTwoDIB(t *testing.T) []byte {
	t.Helper()
	header := make([]byte, infoHeaderBytes)
	binary.LittleEndian.PutUint32(header, infoHeaderBytes)
	binary.LittleEndian.PutUint32(header[widthOffset:], 2)
	binary.LittleEndian.PutUint32(header[heightOffset:], 2)
	binary.LittleEndian.PutUint16(header[bitCountOffset:], bitsTrueColour)
	bottom := []byte{0xFF, 0x00, 0x00, 0x00, 0xFF, 0x00, 0x00, 0x00}
	top := []byte{0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00}
	return append(header, append(bottom, top...)...)
}

func TestPNGFromDIBReadsABottomUpTrueColourBitmap(t *testing.T) {
	encoded, err := pngFromDIB(twoByTwoDIB(t))
	if err != nil {
		t.Fatalf("a 2x2 24 bit bitmap did not decode: %v", err)
	}
	picture, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("the encoded image is not a png: %v", err)
	}
	if bounds := picture.Bounds(); bounds.Dx() != 2 || bounds.Dy() != 2 {
		t.Fatalf("the image is %v, want 2x2", bounds)
	}
	want := map[[2]int]color.RGBA{
		{0, 0}: {R: 0xFF, A: 0xFF},
		{1, 0}: {R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF},
		{0, 1}: {B: 0xFF, A: 0xFF},
		{1, 1}: {G: 0xFF, A: 0xFF},
	}
	for at, expected := range want {
		red, green, blue, alpha := picture.At(at[0], at[1]).RGBA()
		got := color.RGBA{R: uint8(red >> 8), G: uint8(green >> 8), B: uint8(blue >> 8), A: uint8(alpha >> 8)}
		if got != expected {
			t.Errorf("pixel %v is %v, want %v", at, got, expected)
		}
	}
}

func TestPNGFromDIBRefusesAnUnreadableDepth(t *testing.T) {
	raw := twoByTwoDIB(t)
	binary.LittleEndian.PutUint16(raw[bitCountOffset:], 8)
	if _, err := pngFromDIB(raw); err == nil {
		t.Fatal("an 8 bit palette bitmap decoded, and boji does not read palettes")
	}
}

func TestPNGFromDIBRefusesATruncatedBitmap(t *testing.T) {
	raw := twoByTwoDIB(t)
	if _, err := pngFromDIB(raw[:len(raw)-1]); err == nil {
		t.Fatal("a bitmap missing its last byte decoded")
	}
}
