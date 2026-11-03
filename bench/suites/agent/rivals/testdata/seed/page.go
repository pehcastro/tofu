package shop

func Page(items []Item, page, size int) []Item {
	if page < 1 || size < 1 {
		return nil
	}
	start := (page - 1) * size
	end := start + size
	if end > len(items) {
		return nil
	}
	return items[start:end]
}
