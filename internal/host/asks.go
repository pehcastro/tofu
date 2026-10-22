package host

import (
	"errors"
	"io/fs"
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
	held     standingIn
	session  func() standingIn
	unkept   func(error)
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
	read, until, err := now.read()
	var keepErr error
	if errors.Is(err, fs.ErrNotExist) && before.forkedInto(now.id) {
		keepErr = now.keep(a.standing, a.until)
	} else {
		a.standing, a.until = read, until
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
	return listed(a.standing, a.until)
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
	return a.grant(id, given, time.Time{})
}

func (a *asks) grant(id string, given Answer, until time.Time) (answered, stands bool) {
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
		a.standing[asked.place] = given
		if given == AllowedAWhile {
			a.until[asked.place] = until
		}
		keepErr = a.held.keep(a.standing, a.until)
	}
	asked.reply <- given
	a.mu.Unlock()
	a.report(keepErr)
	return true, stands
}

func (a *asks) revoke(place string) (StandingAnswer, bool) {
	a.follow()
	a.mu.Lock()
	now := listed(a.standing, a.until)
	at := slices.IndexFunc(now, func(one StandingAnswer) bool { return one.Target == place })
	if at < 0 {
		a.mu.Unlock()
		return StandingAnswer{}, false
	}
	gone := now[at]
	delete(a.standing, place)
	delete(a.until, place)
	keepErr := a.held.keep(a.standing, a.until)
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
