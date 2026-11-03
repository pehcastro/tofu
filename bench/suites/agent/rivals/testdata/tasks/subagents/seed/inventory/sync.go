package inventory

import (
	"fmt"

	"example.com/depot/retry"
)

type Source interface {
	Level(sku string) (int, error)
}

type Syncer struct {
	source Source
	stock  *Stock
}

func NewSyncer(src Source, stock *Stock) *Syncer {
	return &Syncer{source: src, stock: stock}
}

func (s *Syncer) Pull(sku string) error {
	var level int
	err := retry.Do(syncAttempts, func() error {
		got, err := s.source.Level(sku)
		level = got
		return err
	})
	if err != nil {
		return fmt.Errorf("inventory: level of %s: %w", sku, err)
	}
	s.stock.Set(sku, level)
	return nil
}

func (s *Syncer) PullAll(skus []string) []error {
	var failed []error
	for _, sku := range skus {
		if err := s.Pull(sku); err != nil {
			failed = append(failed, err)
		}
	}
	return failed
}
