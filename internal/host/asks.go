package host

import (
	"slices"
	"sync"
)

type waitingAsk struct {
	id    string
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

func (a *asks) stand(place string, answer Answer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.standing[place] = answer
}

func (a *asks) wait(id string) (<-chan Answer, func()) {
	reply := make(chan Answer, 1)
	a.mu.Lock()
	a.waiting = append(a.waiting, waitingAsk{id: id, reply: reply})
	a.mu.Unlock()
	return reply, func() { a.take(id) }
}

func (a *asks) answer(id string, given Answer) bool {
	reply, waiting := a.take(id)
	if waiting {
		reply <- given
	}
	return waiting
}

func (a *asks) take(id string) (chan Answer, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	at := slices.IndexFunc(a.waiting, func(one waitingAsk) bool { return one.id == id })
	if at < 0 {
		return nil, false
	}
	reply := a.waiting[at].reply
	a.waiting = slices.Delete(a.waiting, at, at+1)
	return reply, true
}
