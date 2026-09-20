//go:build windows

package sys

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strconv"
)

const (
	infoHeaderBytes    = 40
	widthOffset        = 4
	heightOffset       = 8
	bitCountOffset     = 14
	compressionOffset  = 16
	paletteCountOffset = 32
)

const (
	compressionNone      = 0
	compressionBitfields = 3
	bitfieldMaskBytes    = 12
	paletteEntryBytes    = 4
)

const (
	bitsPerByte        = 8
	bitsTrueColour     = 24
	bitsWithAlpha      = 32
	strideAlignmentBit = 32
	strideAlignment    = 4
	fullyOpaque        = 0xFF
	blueOffset         = 0
	greenOffset        = 1
	redOffset          = 2
)

func pngFromDIB(raw []byte) ([]byte, error) {
	if len(raw) < infoHeaderBytes {
		return nil, errors.New("sys: the clipboard bitmap is shorter than its own header")
	}
	headerBytes := int(binary.LittleEndian.Uint32(raw))
	width := int(int32(binary.LittleEndian.Uint32(raw[widthOffset:])))
	signedHeight := int(int32(binary.LittleEndian.Uint32(raw[heightOffset:])))
	bits := int(binary.LittleEndian.Uint16(raw[bitCountOffset:]))
	compression := binary.LittleEndian.Uint32(raw[compressionOffset:])
	paletteCount := int(binary.LittleEndian.Uint32(raw[paletteCountOffset:]))

	if bits != bitsTrueColour && bits != bitsWithAlpha {
		return nil, errors.New("sys: the clipboard bitmap is " + strconv.Itoa(bits) + " bits a pixel, and tofu reads 24 and 32")
	}
	if compression != compressionNone && compression != compressionBitfields {
		return nil, errors.New("sys: the clipboard bitmap is compressed in a way tofu does not read")
	}
	height := signedHeight
	bottomUp := signedHeight > 0
	if !bottomUp {
		height = -signedHeight
	}
	if width <= 0 || height <= 0 || headerBytes < infoHeaderBytes {
		return nil, errors.New("sys: the clipboard bitmap describes no pixels")
	}

	pixels := headerBytes + paletteCount*paletteEntryBytes
	if compression == compressionBitfields && headerBytes == infoHeaderBytes {
		pixels += bitfieldMaskBytes
	}
	stride := (width*bits + strideAlignmentBit - 1) / strideAlignmentBit * strideAlignment
	if len(raw) < pixels+stride*height {
		return nil, errors.New("sys: the clipboard bitmap stops before its last row")
	}

	step := bits / bitsPerByte
	picture := image.NewNRGBA(image.Rect(0, 0, width, height))
	for row := range height {
		source := pixels + row*stride
		target := row
		if bottomUp {
			target = height - 1 - row
		}
		for column := range width {
			pixel := source + column*step
			picture.SetNRGBA(column, target, color.NRGBA{
				R: raw[pixel+redOffset],
				G: raw[pixel+greenOffset],
				B: raw[pixel+blueOffset],
				A: fullyOpaque,
			})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}
