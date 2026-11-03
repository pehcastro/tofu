package llm

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"strings"
)

type FittedImage struct {
	Image
	Was, Sent image.Point
	Shrunk    bool
}

func (i Image) Fitted() Image {
	fit, err := FitImage(i)
	if err != nil {
		return i
	}
	return fit.Image
}

func FitImage(picture Image) (FittedImage, error) {
	fits := base64.StdEncoding.EncodedLen(len(picture.Data)) <= ImageEncodedBytes
	if picture.MediaType == "image/webp" {
		if !fits {
			return FittedImage{}, fmt.Errorf("a webp of %d bytes is past what a model takes and tofu cannot shrink a webp; convert it to png or jpeg", len(picture.Data))
		}
		return FittedImage{Image: picture}, nil
	}
	format := strings.TrimPrefix(picture.MediaType, "image/")
	config, _, err := image.DecodeConfig(bytes.NewReader(picture.Data))
	if err != nil {
		return FittedImage{}, fmt.Errorf("it looks like a %s and does not decode: %w", format, err)
	}
	was := image.Pt(config.Width, config.Height)
	sent := fittedSize(was)
	if fits && sent == was {
		return FittedImage{Image: picture, Was: was, Sent: was}, nil
	}
	decoded, _, err := image.Decode(bytes.NewReader(picture.Data))
	if err != nil {
		return FittedImage{}, fmt.Errorf("it looks like a %s and does not decode: %w", format, err)
	}
	small := shrunk(decoded, sent)
	media, encoded := "image/png", bytes.Buffer{}
	if picture.MediaType != "image/jpeg" {
		err = png.Encode(&encoded, small)
	}
	if picture.MediaType == "image/jpeg" || base64.StdEncoding.EncodedLen(encoded.Len()) > ImageEncodedBytes {
		encoded.Reset()
		media, err = "image/jpeg", jpeg.Encode(&encoded, small, nil)
	}
	if err != nil {
		return FittedImage{}, fmt.Errorf("shrinking it to %dx%d: %w", sent.X, sent.Y, err)
	}
	return FittedImage{Image: Image{MediaType: media, Data: bytes.Clone(encoded.Bytes())}, Was: was, Sent: sent, Shrunk: true}, nil
}

func fittedSize(size image.Point) image.Point {
	if long := max(size.X, size.Y); long > ImageLongEdgePixels {
		size = image.Pt(max(1, size.X*ImageLongEdgePixels/long), max(1, size.Y*ImageLongEdgePixels/long))
	}
	if size.X*size.Y > ImagePixels {
		scale := math.Sqrt(float64(ImagePixels) / float64(size.X*size.Y))
		size = image.Pt(max(1, int(float64(size.X)*scale)), max(1, int(float64(size.Y)*scale)))
	}
	return size
}

func shrunk(picture image.Image, size image.Point) *image.RGBA {
	bounds := picture.Bounds()
	source := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(source, source.Bounds(), picture, bounds.Min, draw.Src)
	out := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
	for y := range size.Y {
		top, bottom := y*bounds.Dy()/size.Y, (y+1)*bounds.Dy()/size.Y
		for x := range size.X {
			left, right := x*bounds.Dx()/size.X, (x+1)*bounds.Dx()/size.X
			var sum [4]int
			for row := top; row < bottom; row++ {
				for i, value := range source.Pix[source.PixOffset(left, row):source.PixOffset(right, row)] {
					sum[i%4] += int(value)
				}
			}
			at, count := out.PixOffset(x, y), (bottom-top)*(right-left)
			for channel, total := range sum {
				out.Pix[at+channel] = uint8(total / count)
			}
		}
	}
	return out
}
