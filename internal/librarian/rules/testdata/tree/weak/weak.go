package weak

import "errors"

var errNoItems = errors.New("weak: no items")

var scale = func() int { return 1 }

type Report struct {
	Total int
}

func Summarise(items []string) (Report, error) {
	if len(items) == 0 {
		return Report{}, errNoItems
	}
	return Report{Total: len(items) * scale()}, nil
}
