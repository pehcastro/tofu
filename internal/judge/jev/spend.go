package jev

import (
	"sync"

	"boji/internal/transport"
)

type SpendMeter struct {
	mu    sync.Mutex
	limit float64
	spent float64
	held  float64
}

func NewSpendMeter(limit float64) *SpendMeter {
	return &SpendMeter{limit: limit}
}

func (m *SpendMeter) Reserve(worstCase float64) error {
	if worstCase < 0 {
		return transport.Fail("jev.Reserve", transport.KindBudget, nil, "a reservation may not be negative")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.spent+m.held+worstCase > m.limit {
		return transport.Fail("jev.Reserve", transport.KindBudget, nil,
			"reserving %.6f would pass the limit of %.6f with %.6f spent and %.6f held",
			worstCase, m.limit, m.spent, m.held)
	}
	m.held += worstCase
	return nil
}

func (m *SpendMeter) Release(worstCase float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.held -= worstCase
	if m.held < 0 {
		m.held = 0
	}
}

func (m *SpendMeter) Settle(worstCase float64, actual float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.held -= worstCase
	if m.held < 0 {
		m.held = 0
	}
	m.spent += actual
}

func (m *SpendMeter) Spent() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.spent
}

func (m *SpendMeter) Remaining() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.limit - m.spent - m.held
}
