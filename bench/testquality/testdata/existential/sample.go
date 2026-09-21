package existential

func Something() *int {
	value := 4
	return &value
}
