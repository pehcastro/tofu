package rule

type Ledger struct {
	fires []Fire
}

func (l *Ledger) Append(f Fire) int {
	l.fires = append(l.fires, f)
	return len(l.fires) - 1
}

func (l *Ledger) Override(i int) {
	l.fires[i].Overridden = true
}

func (l *Ledger) Fires() []Fire {
	return l.fires
}

func (l *Ledger) OverrideRate() (overridden, blocked int) {
	for _, f := range l.fires {
		if !f.Blocked {
			continue
		}
		blocked++
		if f.Overridden {
			overridden++
		}
	}
	return overridden, blocked
}
