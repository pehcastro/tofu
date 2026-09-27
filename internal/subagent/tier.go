package subagent

type Tier string

const (
	TierGenius Tier = "genius"
	TierSmart  Tier = "smart"
	TierWorker Tier = "worker"
	TierDumb   Tier = "dumb"
)

func Tiers() []Tier { return []Tier{TierGenius, TierSmart, TierWorker, TierDumb} }

func (t Tier) Setting() string { return "modelTier." + string(t) }

func tierNames() []string {
	var names []string
	for _, tier := range Tiers() {
		names = append(names, string(tier))
	}
	return names
}

func aliasTier(model string) Tier {
	switch model {
	case "opus":
		return TierGenius
	case "sonnet":
		return TierSmart
	case "haiku":
		return TierWorker
	}
	return ""
}
