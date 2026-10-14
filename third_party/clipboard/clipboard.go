package clipboard

func ReadAll() (string, error) {
	return readAll()
}

func WriteAll(text string) error {
	return writeAll(text)
}

var Unsupported bool
