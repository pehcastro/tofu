package api

import "fmt"

func (sv Service) Freeze(id string) error {
	return sv.setFrozen(id, true)
}

func (sv Service) Thaw(id string) error {
	return sv.setFrozen(id, false)
}

func (sv Service) setFrozen(id string, frozen bool) error {
	a, err := sv.Store.Get(id)
	if err != nil {
		return fmt.Errorf("api: %w", err)
	}
	a.Frozen = frozen
	sv.Store.Put(a)
	return nil
}
