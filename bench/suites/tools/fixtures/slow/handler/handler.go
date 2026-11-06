package handler

import (
	"strconv"
	"strings"
)

type Order struct {
	ID    int
	Items []string
}

func Handle(orders []Order) string {
	var out strings.Builder
	for _, order := range orders {
		out.WriteString(render(order))
		out.WriteString(strconv.FormatUint(checksum(order), 16))
	}
	return out.String()
}

func render(order Order) string {
	line := "order " + strconv.Itoa(order.ID) + ":"
	for _, item := range order.Items {
		line += " " + item
	}
	return line
}

func checksum(order Order) uint64 {
	var sum uint64
	for round := range 2_000_000 {
		for _, item := range order.Items {
			for i := range len(item) {
				sum = sum*31 + uint64(item[i]) + uint64(round)
			}
		}
	}
	return sum
}
