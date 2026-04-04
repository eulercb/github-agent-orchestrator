package statusbar

import (
	"testing"
)

func TestProviderFallback(t *testing.T) {
	called := false
	p := NewProvider("", func() string {
		called = true
		return "fallback text"
	})

	p.Refresh()

	if !called {
		t.Error("fallback function was not called")
	}
	if got := p.Text(); got != "fallback text" {
		t.Errorf("unexpected text: %q", got)
	}
}

func TestProviderCommand(t *testing.T) {
	p := NewProvider("echo hello", nil)
	p.Refresh()

	if got := p.Text(); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestProviderCommandFailsFallsBack(t *testing.T) {
	p := NewProvider("false", func() string {
		return "fallback"
	})
	p.Refresh()

	if got := p.Text(); got != "fallback" {
		t.Errorf("expected 'fallback', got %q", got)
	}
}
