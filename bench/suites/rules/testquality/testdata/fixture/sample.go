package fixture

func Add(a, b int) int {
	return a + b
}

var Save = func(name string) error {
	return nil
}
