package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Result struct {
	Title   string
	URL     string
	Snippet string
}

func (c *Client) Search(ctx context.Context, config Config, query string, count int) ([]Result, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("a search needs a question")
	}
	provider := config.Provider
	if !config.HasSearch() {
		return nil, errors.New("no search provider is configured")
	}
	if count <= 0 || count > provider.MaxResults {
		count = provider.MaxResults
	}
	address, err := url.Parse(provider.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("%s: endpoint %q is not an address", provider.Origin, provider.Endpoint)
	}
	values := address.Query()
	values.Set(provider.QueryParam, query)
	if provider.CountParam != "" {
		values.Set(provider.CountParam, strconv.Itoa(count))
	}
	if provider.KeyParam != "" {
		values.Set(provider.KeyParam, config.searchKey)
	}
	address.RawQuery = values.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if provider.KeyHeader != "" {
		request.Header.Set(provider.KeyHeader, config.searchKey)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s did not answer: %w", provider.Name, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= http.StatusBadRequest {
		return nil, StatusError{URL: provider.Endpoint, Status: response.StatusCode}
	}

	var decoded any
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%s answered with something that is not json: %w", provider.Name, err)
	}
	rows, ok := field(decoded, provider.ResultsPath).([]any)
	if !ok {
		return nil, fmt.Errorf("%s answered without a list at %s, which %s says is where its results are",
			provider.Name, provider.ResultsPath, provider.Origin)
	}
	results := make([]Result, 0, min(len(rows), count))
	for _, row := range rows {
		if len(results) == count {
			break
		}
		found := Result{
			Title:   plain(field(row, provider.TitleField)),
			URL:     plain(field(row, provider.URLField)),
			Snippet: plain(field(row, provider.SnippetField)),
		}
		if found.URL != "" {
			results = append(results, found)
		}
	}
	return results, nil
}

func field(value any, path string) any {
	for _, step := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[step]
	}
	return value
}

func plain(value any) string {
	text, _ := value.(string)
	if !strings.Contains(text, "<") {
		return clean(text)
	}
	return clean(Render(Units("text/html", []byte(text), nil)))
}

func SearchUnits(results []Result) []Unit {
	units := make([]Unit, len(results))
	for i, result := range results {
		units[i] = Unit{Kind: UnitItem, Text: fmt.Sprintf("%d. %s\n   %s\n   %s", i+1, result.Title, result.URL, result.Snippet)}
	}
	return units
}
