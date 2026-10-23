package host

func hubSocketOf(home, name string) (string, error) {
	return tempSocketOf(home, name)
}

func throughSocketDir(socket string, use func(string) error) error {
	return use(socket)
}
