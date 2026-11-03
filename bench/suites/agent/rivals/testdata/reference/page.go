package shop

func Page(items []Item, page, size int) []Item {
	if page < 1 || size < 1 {
		return nil
	}
	start := (page - 1) * size
	if start >= len(items) {
		return nil
	}
	return items[start:min(start+size, len(items))]
}
