package quota

import (
	"errors"
	"testing"
)

func TestManagerReserveRelease(t *testing.T) {
	q, err := New(10)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := q.Reserve("owner-a", 6); err != nil {
		t.Fatalf("Reserve returned error: %v", err)
	}
	if err := q.Reserve("owner-b", 4); err != nil {
		t.Fatalf("Reserve returned error: %v", err)
	}
	if got := q.TotalUsed(); got != 10 {
		t.Fatalf("total used = %d, want 10", got)
	}

	q.Release("owner-a", 2)
	if got := q.Used("owner-a"); got != 4 {
		t.Fatalf("owner-a used = %d, want 4", got)
	}
}

func TestManagerRejectsOverLimit(t *testing.T) {
	q, err := New(4)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := q.Reserve("owner-a", 5); !errors.Is(err, ErrExceeded) {
		t.Fatalf("Reserve error = %v, want ErrExceeded", err)
	}
}
