// Package debug provides a thread-safe, timestamped event log for tracking
// operations within the gao application. Events are displayed in the TUI
// debug pane.
package debug

import (
	"fmt"
	"sync"
	"time"
)

// maxEvents is the maximum number of events retained in the ring buffer.
const maxEvents = 500

// EventStatus represents the lifecycle state of an event.
type EventStatus int

// EventStatus constants.
const (
	// StatusInfo is a one-shot informational event with no duration.
	StatusInfo EventStatus = iota
	// StatusRunning indicates the operation is in progress.
	StatusRunning
	// StatusDone indicates the operation completed successfully.
	StatusDone
	// StatusError indicates the operation failed.
	StatusError
)

// Event represents a single debug log entry.
type Event struct {
	ID        int
	Message   string
	Detail    string // optional extra info shown on completion
	Status    EventStatus
	StartedAt time.Time
	EndedAt   time.Time // zero value when still running
}

// Duration returns the elapsed time. For running events it returns
// time since start; for completed events it returns the total duration.
func (e *Event) Duration() time.Duration {
	if e.EndedAt.IsZero() {
		return time.Since(e.StartedAt)
	}
	return e.EndedAt.Sub(e.StartedAt)
}

// Log is a thread-safe event log that tracks operations.
type Log struct {
	mu     sync.RWMutex
	events []Event
	nextID int
}

// NewLog creates an empty debug log.
func NewLog() *Log {
	return &Log{}
}

// Start records the beginning of a long-running operation and returns
// an event ID that should be passed to Finish or Error.
func (l *Log) Start(msg string) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	id := l.nextID
	l.nextID++

	l.events = append(l.events, Event{
		ID:        id,
		Message:   msg,
		Status:    StatusRunning,
		StartedAt: time.Now(),
	})
	l.trim()
	return id
}

// Finish marks a previously started event as successfully completed.
func (l *Log) Finish(id int, detail string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for i := len(l.events) - 1; i >= 0; i-- {
		if l.events[i].ID == id {
			l.events[i].Status = StatusDone
			l.events[i].EndedAt = time.Now()
			l.events[i].Detail = detail
			return
		}
	}
}

// Error marks a previously started event as failed.
func (l *Log) Error(id int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for i := len(l.events) - 1; i >= 0; i-- {
		if l.events[i].ID == id {
			l.events[i].Status = StatusError
			l.events[i].EndedAt = time.Now()
			if err != nil {
				l.events[i].Detail = err.Error()
			}
			return
		}
	}
}

// Info records a one-shot informational event with no duration.
func (l *Log) Info(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	id := l.nextID
	l.nextID++

	l.events = append(l.events, Event{
		ID:        id,
		Message:   msg,
		Status:    StatusInfo,
		StartedAt: time.Now(),
		EndedAt:   time.Now(),
	})
	l.trim()
}

// Infof records a formatted informational event.
func (l *Log) Infof(format string, args ...any) {
	l.Info(fmt.Sprintf(format, args...))
}

// Events returns a copy of all events, oldest first.
func (l *Log) Events() []Event {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Event, len(l.events))
	copy(out, l.events)
	return out
}

// trim removes oldest non-running events when the buffer exceeds maxEvents.
// Running events are never evicted so that Finish/Error can always find them.
// Must be called with l.mu held.
func (l *Log) trim() {
	for len(l.events) > maxEvents {
		evicted := false
		for i := range l.events {
			if l.events[i].Status != StatusRunning {
				l.events = append(l.events[:i], l.events[i+1:]...)
				evicted = true
				break
			}
		}
		if !evicted {
			// All events are running — nothing safe to evict.
			break
		}
	}
}
