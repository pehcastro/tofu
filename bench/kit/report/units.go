package report

import (
	"strings"

	"tofu/internal/judge/ledger"
)

const CostUnitPrefix = "Cost unit: "

func CostUnitLine(units []ledger.Unit) string {
	present := make(map[ledger.Unit]bool, len(units))
	for _, unit := range units {
		present[unit] = true
	}
	names := make([]string, 0, len(present))
	for _, unit := range ledger.Units() {
		if present[unit] {
			names = append(names, unit.String())
		}
	}
	if len(names) == 0 {
		names = append(names, ledger.UnitUndetermined.String())
	}
	line := CostUnitPrefix + strings.Join(names, ", ") +
		". A money figure is dollars that left the account behind the credential named above, read from the response of the call it names and never from a rate card."
	if len(names) > 1 {
		line += " " + ledger.UnitsDoNotAdd
	}
	return line
}
