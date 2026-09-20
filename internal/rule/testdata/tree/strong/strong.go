package strong

func Clamp(value, limit int) int {
	if value > limit {
		return limit
	}
	if value < 0 {
		return 0
	}
	return value
}
