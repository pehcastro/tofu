package host

import (
	"maps"
	"slices"
	"sync"
)

type waitingAsk struct {
	id    string
	place string
	reply chan Answer
}

type asks struct {
	mu       sync.Mutex
	waiting  []waitingAsk
	standing map[string]Answer
}

func (a *asks) stood(place string) (Answer, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	answer, stands := a.standing[place]
	return answer, stands
}

func (a *asks) standingNow() []StandingAnswer {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := []StandingAnswer{}
	for _, place := range slices.Sorted(maps.Keys(a.standing)) {
		decision := AllowAlways
		if a.standing[place] == NeverHere {
			decision = RejectAlways
		}
		now = append(now, StandingAnswer{Target: place, Decision: decision})
	}
	return now
}

func (a *asks) wait(id, standsAt string) (<-chan Answer, func()) {
	reply := make(chan Answer, 1)
	a.mu.Lock()
	a.waiting = append(a.waiting, waitingAsk{id: id, place: standsAt, reply: reply})
	a.mu.Unlock()
	return reply, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.take(id)
	}
}

func (a *asks) answer(id string, given Answer) (answered, stands bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	asked, waiting := a.take(id)
	if !waiting {
		return false, false
	}
	stands = asked.place != "" && (given == AlwaysHere || given == NeverHere)
	if stands {
		a.standing[asked.place] = given
	}
	asked.reply <- given
	return true, stands
}

func (a *asks) take(id string) (waitingAsk, bool) {
	at := slices.IndexFunc(a.waiting, func(one waitingAsk) bool { return one.id == id })
	if at < 0 {
		return waitingAsk{}, false
	}
	asked := a.waiting[at]
	a.waiting = slices.Delete(a.waiting, at, at+1)
	return asked, true
}
