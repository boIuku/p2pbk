package quota

import (
	"errors"
	"sync"
	"time"
)

var ErrExceeded = errors.New("quota exceeded")

type Manager struct {
	mu       sync.Mutex
	limit    int64
	used     map[string]int64
	updatedAt time.Time
}

func New(limit int64) (*Manager, error) {
	if limit < 0 {
		return nil, errors.New("limit must be non-negative")
	}
	return &Manager{limit: limit, used: map[string]int64{}}, nil
}

func (m *Manager) Reserve(owner string, delta int64) error {
	if owner == "" {
		return errors.New("owner is required")
	}
	if delta < 0 {
		return errors.New("delta must be non-negative")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.limit > 0 && m.total()+delta > m.limit {
		return ErrExceeded
	}
	m.used[owner] += delta
	m.updatedAt = time.Now()
	return nil
}

func (m *Manager) Release(owner string, size int64) {
	if owner == "" || size <= 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.used[owner] -= size
	if m.used[owner] <= 0 {
		delete(m.used, owner)
	}
	m.updatedAt = time.Now()
}

func (m *Manager) Used(owner string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.used[owner]
}

func (m *Manager) Total() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var total int64
	for _, u := range m.used {
		total += u
	}
	return total
}

func (m *Manager) Limit() int64 {
	return m.limit
}

func (m *Manager) total() int64 {
	var total int64
	for _, u := range m.used {
		total += u
	}
	return total
}
