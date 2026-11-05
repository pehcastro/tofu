package quota

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/platform/fs"
)

func HeaderNumber(header http.Header, name string) *float64 {
	number, err := strconv.ParseFloat(strings.TrimSpace(header.Get(name)), 64)
	if err != nil {
		return nil
	}
	return &number
}

func (p *Poller) Heard(account Account, header http.Header) error {
	heard := p.endpoints.of(account.Provider).FromHeaders(header, p.now())
	shelf := p.shelf(account)
	if len(heard.Windows) == 0 || account.AccountID == "" || (p.now().Sub(shelf.read().HeardAt) < heardEvery && !heard.Exhausted()) {
		return nil
	}
	lock, err := shelf.try()
	if lock == nil {
		return err
	}
	defer func() { _ = fs.Unlock(lock) }()
	held := shelf.read()
	merged := held.Report
	merged.Provider, merged.FetchedAt, merged.Source = heard.Provider, heard.FetchedAt, heard.Source
	merged.Windows = slices.Concat(heard.Windows, slices.DeleteFunc(merged.Windows, func(old Window) bool {
		return slices.ContainsFunc(heard.Windows, func(window Window) bool { return window.ID == old.ID })
	}))
	held.Report, held.HeardAt, held.FreshUntil = merged, heard.FetchedAt, heard.FetchedAt.Add(p.freshFor(merged))
	if err := shelf.write(held); err != nil {
		return err
	}
	p.note(account, heard)
	return nil
}
