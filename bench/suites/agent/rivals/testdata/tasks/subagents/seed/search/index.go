package search

import (
	"sort"
	"strings"
	"sync"

	"example.com/depot/retry"
)

var refreshAttempts = defaultRefresh

type Document struct {
	ID   string
	Text string
}

type Loader interface {
	Load() ([]Document, error)
}

type Index struct {
	mu     sync.RWMutex
	loader Loader
	terms  map[string][]string
}

func NewIndex(l Loader) *Index {
	return &Index{loader: l, terms: map[string][]string{}}
}

func (ix *Index) Refresh() error {
	return retry.Do(refreshAttempts, ix.rebuild)
}

func (ix *Index) rebuild() error {
	docs, err := ix.loader.Load()
	if err != nil {
		return err
	}
	terms := map[string][]string{}
	for _, doc := range docs {
		for _, word := range tokens(doc.Text) {
			terms[word] = append(terms[word], doc.ID)
		}
	}
	for word := range terms {
		sort.Strings(terms[word])
	}
	ix.mu.Lock()
	ix.terms = terms
	ix.mu.Unlock()
	return nil
}

func (ix *Index) Lookup(word string) []string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.terms[strings.ToLower(word)]
}
