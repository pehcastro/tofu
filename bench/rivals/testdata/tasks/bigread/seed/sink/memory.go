package sink

import (
	"sync"

	"example.com/atlas/intake"
)

type Memory struct {
	mu     sync.Mutex
	queues map[string][]intake.Order
}

var (
	sharedOnce sync.Once
	shared     *Memory
)

func Shared() *Memory {
	sharedOnce.Do(func() { shared = &Memory{queues: map[string][]intake.Order{}} })
	return shared
}

func (m *Memory) Write(queue string, o intake.Order) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queues[queue] = append(m.queues[queue], o)
	return nil
}

func (m *Memory) Drain(queue string) []intake.Order {
	m.mu.Lock()
	defer m.mu.Unlock()
	orders := m.queues[queue]
	delete(m.queues, queue)
	return orders
}
