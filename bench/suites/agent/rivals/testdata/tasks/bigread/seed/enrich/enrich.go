package enrich

import (
	"strings"

	"example.com/atlas/intake"
)

const (
	defaultRegion = "eu"
	largeCents    = 100000
)

type Stage struct {
	regions map[string]string
}

func New() Stage {
	return Stage{regions: map[string]string{"de": "eu", "fr": "eu", "us": "na", "ca": "na", "jp": "apac"}}
}

func (s Stage) Run(o *intake.Order) error {
	region := strings.ToLower(o.Region)
	if mapped, found := s.regions[region]; found {
		region = mapped
	}
	if region == "" {
		region = defaultRegion
	}
	o.Region = region
	if o.Class == intake.ClassStandard && o.Total() >= largeCents {
		o.Class = intake.ClassUrgent
	}
	return nil
}
