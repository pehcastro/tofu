package notify

import (
	"fmt"
	"sort"
	"strings"
)

var templates = map[string]string{
	"shipped":  "Your order {order} is on its way, tracking {tracking}.",
	"refunded": "We refunded {amount} for order {order}.",
	"welcome":  "Welcome, {name}.",
}

func render(name string, fields map[string]string) (string, error) {
	text, known := templates[name]
	if !known {
		return "", fmt.Errorf("notify: no template %q", name)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		text = strings.ReplaceAll(text, "{"+key+"}", fields[key])
	}
	if strings.Contains(text, "{") {
		return "", fmt.Errorf("notify: template %q has a field left unfilled", name)
	}
	return text, nil
}
