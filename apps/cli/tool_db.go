package main

import (
	"tofu/internal/konst"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/data/db"
)

func newDBTool(dir string) (contract.Tool, error) {
	return db.New(dir, db.Limits{Rows: konst.DBRowsShown, CellBytes: konst.DBCellBytes, QueryMillis: konst.DBQueryMillis})
}
