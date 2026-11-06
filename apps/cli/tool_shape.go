package main

import (
	"tofu/internal/konst"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/data/shape"
)

func newShapeTool(dir string) (contract.Tool, error) {
	return shape.New(dir, shape.Limits{Bytes: konst.ShapeSampleBytes, Context: konst.ShapeErrorContextBytes})
}
