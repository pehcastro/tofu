package shipping

import (
	"fmt"
	"strings"
)

type Zone int

const (
	ZoneDomestic Zone = iota
	ZoneEurope
	ZoneWorld
)

type Label struct {
	Tracking string
	Zone     Zone
	Grams    int
}

var europe = []string{"AT", "BE", "DE", "DK", "ES", "FI", "FR", "IE", "IT", "NL", "PL", "PT", "SE"}

func zoneOf(country string) Zone {
	country = strings.ToUpper(country)
	switch {
	case country == "DE":
		return ZoneDomestic
	case contains(europe, country):
		return ZoneEurope
	}
	return ZoneWorld
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func makeLabel(p Parcel) Label {
	zone := zoneOf(p.Country)
	return Label{Tracking: fmt.Sprintf("%s%d-%s", trackingPrefix, zone, p.Order), Zone: zone, Grams: p.Grams}
}
