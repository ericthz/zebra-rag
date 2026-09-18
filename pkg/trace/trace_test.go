package trace

import (
	"context"
	"testing"
)

func TestWithAndFromContext(t *testing.T) {
	ctx := context.Background()
	if FromContext(ctx) != "" {
		t.Error("empty context should return empty id")
	}
	ctx = WithID(ctx, "abc123")
	if FromContext(ctx) != "abc123" {
		t.Errorf("FromContext = %q, want abc123", FromContext(ctx))
	}
}

func TestNewID(t *testing.T) {
	id := NewID()
	if len(id) != 32 {
		t.Errorf("NewID length = %d, want 32", len(id))
	}
	if id == NewID() {
		t.Error("two generated IDs should differ")
	}
}
