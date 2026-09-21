package jev

import (
	"encoding/json"
	"strconv"

	"tofu/internal/transport"
)

type Legend []string

func decodeLegend(id string, raw json.RawMessage) (Legend, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var wire map[string]string
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, transport.Fail("jev.Decode", transport.KindInvalidAnswer, err,
			"answer %q carries a legend that is not a level to word object", id)
	}
	legend := make(Legend, len(wire))
	for key, word := range wire {
		level, err := strconv.Atoi(key)
		if err != nil || level < 0 || level >= len(wire) {
			return nil, transport.Fail("jev.Decode", transport.KindInvalidAnswer, nil,
				"answer %q has legend level %q, and the legend holds %d levels", id, key, len(wire))
		}
		legend[level] = word
	}
	for level, word := range legend {
		if word == "" {
			return nil, transport.Fail("jev.Decode", transport.KindInvalidAnswer, nil,
				"answer %q has no legend word for level %d", id, level)
		}
	}
	return legend, nil
}
