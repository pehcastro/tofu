package widget

import "strconv"

const (
	millionTokens  = 1_000_000
	thousandTokens = 1_000
)

func Weighted(tokens float64) string {
	switch {
	case tokens >= millionTokens:
		return strconv.FormatFloat(tokens/millionTokens, 'f', 1, 64) + "M"
	case tokens >= thousandTokens:
		return strconv.FormatFloat(tokens/thousandTokens, 'f', 0, 64) + "k"
	}
	return strconv.FormatFloat(tokens, 'f', 0, 64)
}
