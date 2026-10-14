package inventory

import (
	"sort"
	"sync"
)

const (
	syncAttempts = 5
	lowWater     = 3
)

type Stock struct {
	mu     sync.Mutex
	levels map[string]int
}

func NewStock() *Stock {
	return &Stock{levels: map[string]int{}}
}

func (s *Stock) Set(sku string, level int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.levels[sku] = level
}

func (s *Stock) Level(sku string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.levels[sku]
}

func (s *Stock) Low() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var low []string
	for sku, level := range s.levels {
		if level <= lowWater {
			low = append(low, sku)
		}
	}
	sort.Strings(low)
	return low
}
