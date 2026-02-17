package cache

import (
	"testing"
	"time"
)

func TestMemorySetAndGet(t *testing.T) {
	t.Parallel()

	c := NewMemory(100 * time.Millisecond)
	c.Set("a", 123)

	got, ok := c.Get("a")
	if !ok {
		t.Fatalf("expected key to exist")
	}
	if got.(int) != 123 {
		t.Fatalf("unexpected value: %v", got)
	}
}

func TestMemoryGetExpired(t *testing.T) {
	t.Parallel()

	c := NewMemory(20 * time.Millisecond)
	c.Set("a", 123)
	time.Sleep(40 * time.Millisecond)

	if _, ok := c.Get("a"); ok {
		t.Fatalf("expected expired key to be removed")
	}
}

func TestMemoryGetMissing(t *testing.T) {
	t.Parallel()

	c := NewMemory(1 * time.Second)
	if _, ok := c.Get("missing"); ok {
		t.Fatalf("expected missing key to return ok=false")
	}
}
