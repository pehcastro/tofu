package motion

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"

	"tofu/internal/konst"
)

const (
	glyphChars  = "0123456789+-#ms "
	glyphWidth  = 5
	glyphHeight = 7
	glyphRows   = `
.XXX. ..X.. .XXX. XXXX. ...X. XXXXX ..XX. XXXXX .XXX. .XXX. ..... ..... .X.X. ..... ..... .....
X...X .XX.. X...X ....X ..XX. X.... .X... ....X X...X X...X ..X.. ..... .X.X. ..... ..... .....
X..XX ..X.. ....X ....X .X.X. XXXX. X.... ...X. X...X X...X ..X.. ..... XXXXX XX.X. .XXXX .....
X.X.X ..X.. ...X. .XXX. X..X. ....X XXXX. ..X.. .XXX. .XXXX XXXXX XXXXX .X.X. X.X.X X.... .....
XX..X ..X.. ..X.. ....X XXXXX ....X X...X .X... X...X ....X ..X.. ..... XXXXX X.X.X .XXX. .....
X...X ..X.. .X... ....X ...X. X...X X...X .X... X...X ...X. ..X.. ..... .X.X. X.X.X ....X .....
.XXX. .XXX. XXXXX XXXX. ...X. .XXX. .XXX. .X... .XXX. .XX.. ..... ..... .X.X. X.X.X XXXX. .....`
)

func labelTextX(i, scale int) int {
	return konst.MotionLabelInsetPx + scale + i*(glyphWidth+1)*scale
}

func drawLabel(img *image.RGBA, text string, scale int) {
	rows := strings.Split(strings.TrimPrefix(glyphRows, "\n"), "\n")
	inset := konst.MotionLabelInsetPx
	box := image.Rect(inset, inset, labelTextX(len(text), scale), inset+(glyphHeight+2)*scale)
	draw.DrawMask(img, box, image.Black, image.Point{}, image.NewUniform(color.Alpha{A: konst.MotionLabelShadeAlpha}), image.Point{}, draw.Over)
	for i, r := range text {
		g := strings.IndexRune(glyphChars, r)
		if g < 0 {
			panic(fmt.Sprintf("motion: no glyph for %q", r))
		}
		for gy, row := range rows {
			for gx := range glyphWidth {
				if row[g*(glyphWidth+1)+gx] != 'X' {
					continue
				}
				dot := image.Rect(0, 0, scale, scale).Add(image.Pt(labelTextX(i, scale)+gx*scale, inset+(gy+1)*scale))
				draw.Draw(img, dot, image.White, image.Point{}, draw.Src)
			}
		}
	}
}
