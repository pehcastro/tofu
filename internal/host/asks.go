package host

import (
	"slices"
	"sync"
	"time"
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
	until    map[string]time.Time
	given    map[string]time.Time
	held     standingIn
	session  func() standingIn
	unkept   func(error)
	granted  func(id string, said Resolution)
}

func (a *asks) follow() {
	if a.session == nil {
		return
	}
	now := a.session()
	a.mu.Lock()
	if now.id == a.held.id {
		a.mu.Unlock()
		return
	}
	before := a.held
	a.held = now
	var keepErr error
	if now.unwrittenForkOf(before) {
		keepErr = now.keep(a.standing, a.until, a.given)
	} else {
		a.standing, a.until, a.given, _ = now.read()
	}
	a.mu.Unlock()
	a.report(keepErr)
}

func (a *asks) report(keepErr error) {
	if keepErr != nil && a.unkept != nil {
		a.unkept(keepErr)
	}
}

func (a *asks) stood(place string) (Answer, bool) {
	a.follow()
	a.mu.Lock()
	defer a.mu.Unlock()
	answer, stands := a.standing[place]
	if answer == AllowedAWhile && !time.Now().Before(a.until[place]) {
		return Denied, false
	}
	return answer, stands
}

func (a *asks) standingNow() []StandingAnswer {
	a.follow()
	a.mu.Lock()
	defer a.mu.Unlock()
	return listed(a.standing, a.until, a.given)
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

func (a *asks) holding(id string, do func()) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	held := slices.ContainsFunc(a.waiting, func(one waitingAsk) bool { return one.id == id })
	if held {
		do()
	}
	return held
}

func (a *asks) grant(id string, given Answer, until time.Time, said *Resolution) (answered, stands bool) {
	a.follow()
	a.mu.Lock()
	asked, waiting := a.take(id)
	if !waiting {
		a.mu.Unlock()
		return false, false
	}
	stands = asked.place != "" && (given == AlwaysHere || given == NeverHere || given == AllowedAWhile)
	var keepErr error
	if stands {
		a.standing[asked.place], a.given[asked.place] = given, time.Now().UTC()
		if given == AllowedAWhile {
			a.until[asked.place] = until
		}
		keepErr = a.held.keep(a.standing, a.until, a.given)
	}
	if said != nil {
		bound := *said
		bound.Standing, bound.Until = stands, until
		a.granted(id, bound)
	}
	asked.reply <- given
	a.mu.Unlock()
	a.report(keepErr)
	return true, stands
}

func (a *asks) revoke(place string) (StandingAnswer, bool) {
	a.follow()
	a.mu.Lock()
	now := listed(a.standing, a.until, a.given)
	at := slices.IndexFunc(now, func(one StandingAnswer) bool { return one.Target == place })
	if at < 0 {
		a.mu.Unlock()
		return StandingAnswer{}, false
	}
	gone := now[at]
	delete(a.standing, place)
	delete(a.until, place)
	delete(a.given, place)
	keepErr := a.held.keep(a.standing, a.until, a.given)
	a.mu.Unlock()
	a.report(keepErr)
	return gone, true
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
