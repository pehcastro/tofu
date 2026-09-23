package tokens

import (
	"encoding/json"
	"sort"

	"tofu/bench/schemas"
	"tofu/internal/llm"
)

type ToolProse struct {
	Name        string
	DescBytes   int
	ParamBytes  int
	SchemaBytes int
}

type ProseWhole struct {
	Tools           []ToolProse
	DescBytes       int
	ParamBytes      int
	StructuralBytes int
	SchemaBytes     int
}

func MeasureProse(defs []llm.Tool, whole schemas.Whole) ProseWhole {
	byName := map[string]int{}
	for _, tc := range whole.Tools {
		byName[tc.Name] = tc.Bytes
	}
	pw := ProseWhole{Tools: make([]ToolProse, len(defs)), SchemaBytes: whole.WireBytes}
	for i, d := range defs {
		desc := len(d.Description)
		param := 0
		if encoded, err := json.Marshal(d.Parameters); err == nil {
			param = len(encoded)
		}
		pw.Tools[i] = ToolProse{Name: d.Name, DescBytes: desc, ParamBytes: param, SchemaBytes: byName[d.Name]}
		pw.DescBytes += desc
		pw.ParamBytes += param
	}
	pw.StructuralBytes = pw.SchemaBytes - pw.DescBytes - pw.ParamBytes
	return pw
}

func (pw ProseWhole) ProseShare() float64 {
	if pw.SchemaBytes == 0 {
		return 0
	}
	return 100 * float64(pw.DescBytes) / float64(pw.SchemaBytes)
}

func (pw ProseWhole) SortedByDescBytes() []ToolProse {
	sorted := make([]ToolProse, len(pw.Tools))
	copy(sorted, pw.Tools)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].DescBytes > sorted[j].DescBytes })
	return sorted
}
