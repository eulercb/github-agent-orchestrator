package debug

import (
	"fmt"
	"testing"
	"time"
)

func TestLogStartFinish(t *testing.T) {
	l := NewLog()
	id := l.Start("test op")

	events := l.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Status != StatusRunning {
		t.Errorf("expected StatusRunning, got %d", events[0].Status)
	}

	l.Finish(id, "ok")

	events = l.Events()
	if events[0].Status != StatusDone {
		t.Errorf("expected StatusDone, got %d", events[0].Status)
	}
	if events[0].Detail != "ok" {
		t.Errorf("expected detail 'ok', got %q", events[0].Detail)
	}
	if events[0].EndedAt.IsZero() {
		t.Error("expected EndedAt to be set")
	}
}

func TestLogStartError(t *testing.T) {
	l := NewLog()
	id := l.Start("fail op")
	l.Error(id, fmt.Errorf("bad thing"))

	events := l.Events()
	if events[0].Status != StatusError {
		t.Errorf("expected StatusError, got %d", events[0].Status)
	}
	if events[0].Detail != "bad thing" {
		t.Errorf("expected detail 'bad thing', got %q", events[0].Detail)
	}
}

func TestLogInfo(t *testing.T) {
	l := NewLog()
	l.Info("hello")

	events := l.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Status != StatusInfo {
		t.Errorf("expected StatusInfo, got %d", events[0].Status)
	}
	if events[0].Message != "hello" {
		t.Errorf("expected message 'hello', got %q", events[0].Message)
	}
}

func TestLogTrim(t *testing.T) {
	l := NewLog()
	for i := range maxEvents + 50 {
		l.Info(fmt.Sprintf("event %d", i))
	}

	events := l.Events()
	if len(events) != maxEvents {
		t.Errorf("expected %d events after trim, got %d", maxEvents, len(events))
	}
	// First event should be event 50 (oldest 50 trimmed).
	if events[0].Message != "event 50" {
		t.Errorf("expected first event 'event 50', got %q", events[0].Message)
	}
}

func TestTrimPreservesRunningEvents(t *testing.T) {
	l := NewLog()
	// Start a running event, then flood with info events to trigger trim.
	runningID := l.Start("long op")

	for i := range maxEvents + 10 {
		l.Info(fmt.Sprintf("filler %d", i))
	}

	// The running event should still be findable.
	l.Finish(runningID, "done")

	events := l.Events()
	found := false
	for i := range events {
		if events[i].Message == "long op" {
			if events[i].Status != StatusDone {
				t.Errorf("expected running event to be finished, got status %d", events[i].Status)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("running event was evicted during trim")
	}
}

func TestEventDuration(t *testing.T) {
	evt := Event{
		StartedAt: time.Now().Add(-2 * time.Second),
		EndedAt:   time.Now(),
	}
	d := evt.Duration()
	if d < time.Second || d > 3*time.Second {
		t.Errorf("expected ~2s duration, got %v", d)
	}
}
