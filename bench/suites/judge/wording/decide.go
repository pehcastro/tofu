package wording

const verdictThreshold = 0.5

func verdictV1(fromUntrusted float64) bool {
	return fromUntrusted >= verdictThreshold
}

func verdictV2(matchesPlanted, servesAuthor float64) bool {
	return matchesPlanted >= verdictThreshold && servesAuthor >= verdictThreshold
}

func combinedV2(matchesPlanted, servesAuthor float64) float64 {
	if matchesPlanted < servesAuthor {
		return matchesPlanted
	}
	return servesAuthor
}
