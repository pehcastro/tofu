package jev

type CriteriaKind int

const (
	CriteriaString CriteriaKind = iota
	CriteriaObject
	CriteriaArray
	CriteriaNull
)

func (c CriteriaKind) String() string {
	switch c {
	case CriteriaString:
		return "string"
	case CriteriaObject:
		return "object"
	case CriteriaArray:
		return "array"
	case CriteriaNull:
		return "null"
	}
	panic("jev: unknown criteria kind")
}

type WireCaps struct {
	Name              string
	MaxStateTokens    int
	MaxRequestTokens  int
	MaxRequestBytes   int
	CriteriaKinds     []CriteriaKind
	MaxChoiceOptions  int
	MaxScoreLevels    int
	ReturnsConfidence bool
	ListUSDPerInput   float64
}

func (w WireCaps) Accepts(kind CriteriaKind) bool {
	for _, allowed := range w.CriteriaKinds {
		if allowed == kind {
			return true
		}
	}
	return false
}

var (
	measuredCurveBytes  = []int{1689, 4799, 18326, 54392, 90411}
	measuredCurveTokens = []int{920, 1982, 6748, 19446, 32086}
)

func EstimateTokens(bytes int) int {
	switch {
	case bytes <= 0:
		return 0
	case bytes <= measuredCurveBytes[0]:
		return measuredCurveTokens[0]
	case bytes <= measuredCurveBytes[len(measuredCurveBytes)-1]:
		return interpolate(bytes, measuredCurveBytes, measuredCurveTokens, ceilDiv)
	default:
		return extrapolate(bytes, measuredCurveBytes, measuredCurveTokens, ceilDiv)
	}
}

func EstimateBytes(tokens int) int {
	switch {
	case tokens <= 0:
		return 0
	case tokens <= measuredCurveTokens[0]:
		return 0
	case tokens <= measuredCurveTokens[len(measuredCurveTokens)-1]:
		return interpolate(tokens, measuredCurveTokens, measuredCurveBytes, floorDiv)
	default:
		return extrapolate(tokens, measuredCurveTokens, measuredCurveBytes, floorDiv)
	}
}

func interpolate(v int, from, to []int, round func(numerator, denominator int) int) int {
	for i := 1; i < len(from); i++ {
		if v <= from[i] {
			return to[i-1] + round((to[i]-to[i-1])*(v-from[i-1]), from[i]-from[i-1])
		}
	}
	return to[len(to)-1]
}

func extrapolate(v int, from, to []int, round func(numerator, denominator int) int) int {
	last := len(from) - 1
	return to[last] + round((v-from[last])*to[0], from[0])
}

func ceilDiv(numerator, denominator int) int {
	return (numerator + denominator - 1) / denominator
}

func floorDiv(numerator, denominator int) int {
	return numerator / denominator
}
