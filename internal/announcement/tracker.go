package announcement

import (
	"sync"
)

type PendingAnnouncement struct {
	ID       int
	Title    string
	Message  string
	Priority string
}

type Tracker struct {
	mu      sync.Mutex
	pending map[int]*PendingAnnouncement
}

func NewTracker() *Tracker {
	return &Tracker{pending: make(map[int]*PendingAnnouncement)}
}

func (t *Tracker) Add(id int, title, message, priority string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending[id] = &PendingAnnouncement{
		ID:       id,
		Title:    title,
		Message:  message,
		Priority: priority,
	}
}

func (t *Tracker) Remove(id int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.pending, id)
}

func (t *Tracker) GetCriticalPending() []*PendingAnnouncement {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]*PendingAnnouncement, 0)
	for _, ann := range t.pending {
		if ann == nil {
			continue
		}
		if ann.Priority != "critical" {
			continue
		}
		copy := *ann
		out = append(out, &copy)
	}
	return out
}
