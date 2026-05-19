package quota

import (
	"errors"
	"fmt"
	"sync"
)

var ErrExceeded = errors.New("quota exceeded")

type Manager struct {
	mu         sync.Mutex
	limitBytes int64
	used       map[string]int64
}

func New(limitBytes int64) (*Manager, error) {
	if limitBytes < 0 {
		return nil, errors.New("limitBytes must be non-negative")
	}
	return &Manager{limitBytes: limitBytes, used: map[string]int64{}}, nil
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

	if m.limitBytes > 0 && m.totalUsedLocked()+delta > m.limitBytes {
		return fmt.Errorf("%w: used=%d delta=%d limit=%d", ErrExceeded, m.totalUsedLocked(), delta, m.limitBytes)
	}
	m.used[owner] += delta
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
}

func (m *Manager) Used(owner string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.used[owner]
}

func (m *Manager) TotalUsed() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.totalUsedLocked()
}

func (m *Manager) Limit() int64 {
	return m.limitBytes
}

func (m *Manager) totalUsedLocked() int64 {
	var total int64
	for _, used := range m.used {
		total += used
	}
	return total
}
