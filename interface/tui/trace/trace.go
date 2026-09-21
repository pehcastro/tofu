package trace

const shortRunes = 6

func Short(id string) string {
	if id == "" {
		return ""
	}
	runes := []rune(id)
	if len(runes) > shortRunes {
		runes = runes[len(runes)-shortRunes:]
	}
	return "#" + string(runes)
}
