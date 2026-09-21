package update

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"

	"tofu/internal/transport"
)

const (
	SourceURLVariable    = "TOFU_UPDATE_URL"
	AttemptTimeoutMillis = 5000
)

type Result struct {
	Available bool
	Current   string
	Latest    string
	Restart   bool
}

func Source() string {
	return strings.TrimSpace(os.Getenv(SourceURLVariable))
}

func Run(ctx context.Context, client *transport.Client, url, current string) Result {
	result, err := Check(ctx, client, url, current)
	if err != nil {
		return Result{Current: current}
	}
	return result
}

func Check(ctx context.Context, client *transport.Client, url, current string) (Result, error) {
	if url == "" {
		return Result{Current: current}, nil
	}
	response, err := client.Do(ctx, transport.Request{
		Method: http.MethodGet,
		URL:    url,
		Header: http.Header{"Accept": []string{"application/json"}},
	})
	if err != nil {
		return Result{}, err
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return Result{}, err
	}
	latest := strings.TrimSpace(payload.Version)
	if latest == "" {
		return Result{Current: current}, nil
	}
	return Result{
		Available: newer(latest, current),
		Current:   current,
		Latest:    latest,
	}, nil
}

func newer(latest, current string) bool {
	left, right := versionParts(latest), versionParts(current)
	for index := 0; index < len(left) || index < len(right); index++ {
		var l, r int
		if index < len(left) {
			l = left[index]
		}
		if index < len(right) {
			r = right[index]
		}
		if l != r {
			return l > r
		}
	}
	return false
}

func versionParts(version string) []int {
	fields := strings.Split(version, ".")
	parts := make([]int, len(fields))
	for index, field := range fields {
		parts[index], _ = strconv.Atoi(field)
	}
	return parts
}
