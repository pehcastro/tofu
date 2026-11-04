package fire

import (
	"sort"

	"tofu/internal/librarian/rules"
)

type CatalogRule struct {
	ID   string
	Mode string
}

func StructuralCatalog(libraryDir string) ([]CatalogRule, error) {
	loaded, err := rules.LoadDir(libraryDir)
	if err != nil {
		return nil, err
	}
	var structural []CatalogRule
	for _, one := range loaded {
		if one.Kind != rules.KindStructural {
			continue
		}
		structural = append(structural, CatalogRule{ID: one.ID, Mode: one.Mode.String()})
	}
	sort.Slice(structural, func(i, j int) bool { return structural[i].ID < structural[j].ID })
	return structural, nil
}
