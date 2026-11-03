package validate

import (
	"fmt"

	"example.com/atlas/intake"
)

type Stage struct{}

func (Stage) Run(o *intake.Order) error {
	if len(o.Lines) == 0 {
		return intake.ErrEmpty
	}
	for i, line := range o.Lines {
		if line.SKU == "" || line.Quantity <= 0 || line.UnitCents < 0 {
			return fmt.Errorf("validate: line %d of order %s is malformed", i, o.ID)
		}
	}
	return nil
}
