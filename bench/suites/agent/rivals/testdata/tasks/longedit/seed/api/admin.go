package api

import "fmt"

func (sv Service) Freeze(id string) error {
	return sv.setFrozen(id, true)
}

func (sv Service) Thaw(id string) error {
	return sv.setFrozen(id, false)
}

func (sv Service) setFrozen(id string, frozen bool) error {
	a, ok := sv.Store.Get(id)
	if !ok {
		return fmt.Errorf("api: no account %q", id)
	}
	a.Frozen = frozen
	sv.Store.Put(a)
	return nil
}
