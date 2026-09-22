package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tofu/internal/web"
)

const braveShapedAnswer = `{"query":{"original":"go worker pool"},"web":{"results":[
	{"title":"Worker pools","url":"https://acme.example/docs/pools","description":"how to <strong>size</strong> a pool"},
	{"title":"Queues","url":"https://acme.example/docs/queues","description":"where jobs arrive"},
	{"title":"Metrics","url":"https://acme.example/docs/metrics","description":"what to watch"}]}}`

func searchAgainst(t *testing.T, answer string) (web.Config, func() *http.Request) {
	t.Helper()
	var asked *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		asked = request
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(answer))
	}))
	t.Cleanup(server.Close)

	t.Setenv(absentKey, "a-key-that-is-never-printed")
	config, err := web.Load([]web.Layer{shipped(), project(map[string]string{
		"search/brave.yaml": "endpoint: " + server.URL + "/res/v1/web/search\nkey_variable: " + absentKey + "\n",
	})})
	if err != nil {
		t.Fatalf("loading the provider: %v", err)
	}
	return config, func() *http.Request { return asked }
}

func TestSearchAsksTheLibraryProviderAndReturnsWhatItRanked(t *testing.T) {
	config, _ := searchAgainst(t, braveShapedAnswer)
	results, err := web.NewClient(config).Search(context.Background(), config, "go worker pool", 2)
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	t.Logf("results:\n%s", web.Render(web.SearchUnits(results)))
	if len(results) != 2 {
		t.Fatalf("count was not honoured: %d results", len(results))
	}
	if results[0].URL != "https://acme.example/docs/pools" || results[0].Title != "Worker pools" {
		t.Fatalf("the first result was not read out of the answer: %+v", results[0])
	}
	if results[0].Snippet != "how to size a pool" {
		t.Fatalf("the snippet still carries markup: %q", results[0].Snippet)
	}
}

func TestSearchSendsTheKeyInTheHeaderTheLibraryNames(t *testing.T) {
	config, latest := searchAgainst(t, braveShapedAnswer)
	if _, err := web.NewClient(config).Search(context.Background(), config, "go worker pool", 0); err != nil {
		t.Fatalf("searching: %v", err)
	}
	asked := latest()
	t.Logf("the provider was asked %s with the key header present: %t",
		asked.URL.String(), asked.Header.Get("X-Subscription-Token") != "")
	if asked.Header.Get("X-Subscription-Token") != "a-key-that-is-never-printed" {
		t.Fatalf("the key did not travel in the header the library names: %v", asked.Header)
	}
	if asked.URL.Query().Get("q") != "go worker pool" || asked.URL.Query().Get("count") != "10" {
		t.Fatalf("the query did not go in the parameters the library names: %s", asked.URL.RawQuery)
	}
}

func TestAnAnswerWithoutTheRankedListSaysWhereItLooked(t *testing.T) {
	config, _ := searchAgainst(t, `{"error":"over quota"}`)
	_, err := web.NewClient(config).Search(context.Background(), config, "go worker pool", 0)
	t.Logf("a provider answering something else: %v", err)
	if err == nil || !strings.Contains(err.Error(), "web.results") {
		t.Fatalf("the failure does not name the path the library gave: %v", err)
	}
	if strings.Contains(err.Error(), "a-key-that-is-never-printed") {
		t.Fatal("the failure carries the key")
	}
}
