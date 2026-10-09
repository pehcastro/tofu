import json
import sys

from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.ttLib import TTFont


def drawn(font_path, fill, points):
    font = TTFont(font_path)
    cmap = font.getBestCmap()
    glyphs = font.getGlyphSet()
    top = font["hhea"].ascent
    bottom = font["hhea"].descent
    height = top - bottom
    found = {}
    for point in points:
        name = cmap.get(int(point, 16))
        if name is None:
            raise SystemExit(f"{font_path} has no glyph for {point}")
        pen = SVGPathPen(glyphs)
        glyphs[name].draw(pen)
        width = glyphs[name].width
        found[point] = (
            f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 {-top} {width} {height}">'
            f'<path fill="{fill}" transform="scale(1 -1)" d="{pen.getCommands()}"/></svg>'
        )
    return found


if __name__ == "__main__":
    font_path, fill, *points = sys.argv[1:]
    print(json.dumps(drawn(font_path, fill, points)))
