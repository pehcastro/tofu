package tea

import (
	"github.com/charmbracelet/x/ansi"
)

type KeyboardEnhancementsMsg struct {
	Flags int
}

func (k KeyboardEnhancementsMsg) SupportsKeyDisambiguation() bool {
	return k.Flags > 0
}

func (k KeyboardEnhancementsMsg) SupportsEventTypes() bool {
	return k.Flags&ansi.KittyReportEventTypes != 0
}

func (k KeyboardEnhancementsMsg) SupportsAlternateKeys() bool {
	return k.Flags&ansi.KittyReportAlternateKeys != 0
}

func (k KeyboardEnhancementsMsg) SupportsAllKeysAsEscapeCodes() bool {
	return k.Flags&ansi.KittyReportAllKeysAsEscapeCodes != 0
}

func (k KeyboardEnhancementsMsg) SupportsAssociatedText() bool {
	return k.Flags&ansi.KittyReportAssociatedKeys != 0
}
