package turn

import (
	"fmt"
	"time"
)

const (
	MechanismRefuse   = "refuse"
	MechanismRepair   = "repair"
	MechanismRemember = "remember"
)

type Standing string

const (
	Kept         Standing = "kept"
	NotKept      Standing = "not kept"
	Unmeasurable Standing = "not measurable on this corpus"
)

type Mechanism struct {
	Name               string
	RoundTripsSaved    int
	WallClockSavedMS   float64
	BytesReturnedDelta int64
	AnswersChanged     int
	Standing           Standing
	Verdict            string
}

var memoWiredAt = time.Date(2026, 9, 20, 9, 29, 35, 0, time.FixedZone("-03:00", -3*60*60))

func anyTurnAfter(turns []RecordedTurn, cutoff time.Time) bool {
	for _, t := range turns {
		if t.At.After(cutoff) {
			return true
		}
	}
	return false
}

func sumArms(arms [][2]ReplayArm) (asRecorded, layered ReplayArm) {
	for _, pair := range arms {
		a, l := pair[0], pair[1]
		asRecorded.Steps += a.Steps
		asRecorded.Calls += a.Calls
		asRecorded.Refusals += a.Refusals
		asRecorded.BytesReturned += a.BytesReturned
		asRecorded.WallClockMS += a.WallClockMS
		layered.Steps += l.Steps
		layered.Calls += l.Calls
		layered.CacheHits += l.CacheHits
		layered.Retries += l.Retries
		layered.Repaired += l.Repaired
		layered.Refusals += l.Refusals
		layered.Undecided += l.Undecided
		layered.BytesReturned += l.BytesReturned
		layered.AnswersChanged += l.AnswersChanged
		layered.WallClockMS += l.WallClockMS
	}
	return asRecorded, layered
}

func cacheHitsAmong(turns []TurnReplay, ids []string) int {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	hits := 0
	for _, t := range turns {
		if want[t.ID] {
			hits += t.Layered.CacheHits
		}
	}
	return hits
}

func meanMSPerCall(asRecorded ReplayArm) float64 {
	if asRecorded.Calls == 0 {
		return 0
	}
	return asRecorded.WallClockMS / float64(asRecorded.Calls)
}

func mechanismTotals(asRecorded, layered ReplayArm, skipped BytesSkipped, memoEraCovered bool) []Mechanism {
	perCall := meanMSPerCall(asRecorded)

	remember := Mechanism{
		Name:               MechanismRemember,
		RoundTripsSaved:    layered.CacheHits,
		WallClockSavedMS:   float64(layered.CacheHits) * perCall,
		BytesReturnedDelta: -skipped.ByCache,
		AnswersChanged:     layered.CacheHits,
	}
	switch {
	case !memoEraCovered:
		remember.Standing = Unmeasurable
		remember.Verdict = "not measurable on this corpus: no recorded turn postdates the memo's own wiring commit `81e2f47`, so the mechanism has never had a chance to fire, see Remember, measured over its own era below"
	case remember.RoundTripsSaved > 0:
		remember.Standing = Kept
		remember.Verdict = fmt.Sprintf("earned its place: %d round trips saved, the number that decided it", remember.RoundTripsSaved)
	default:
		remember.Standing = NotKept
		remember.Verdict = "did not fire once across the turns recorded after the memo existed: no side-effect-free call repeated its exact key inside one turn, the number that decided it"
	}

	repair := Mechanism{
		Name:               MechanismRepair,
		RoundTripsSaved:    layered.Retries,
		WallClockSavedMS:   float64(layered.Retries) * perCall,
		BytesReturnedDelta: -skipped.ByRetry,
		AnswersChanged:     layered.Repaired,
	}
	if repair.AnswersChanged > 0 {
		repair.Standing = Kept
		repair.Verdict = fmt.Sprintf("earned its place: %d calls that failed in the record now succeed, the number that decided it", repair.AnswersChanged)
	} else {
		repair.Standing = NotKept
		repair.Verdict = "did not fire once across this corpus: no recorded failure matched the repairable shape, the number that decided it"
	}

	refuse := Mechanism{Name: MechanismRefuse}
	if layered.Refusals > 0 {
		refuse.Standing = Kept
		refuse.Verdict = fmt.Sprintf("earned its place on existence evidence, not on any of the four numbers above, which are all zero by construction because both arms already show the same refusal text: %d refusal-shaped edit failures occurred in real sessions, the number that decided it", layered.Refusals)
	} else {
		refuse.Standing = NotKept
		refuse.Verdict = "no refusal-shaped failure occurred in this corpus, the number that decided it"
	}

	return []Mechanism{refuse, repair, remember}
}
