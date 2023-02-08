package concurrency

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestExample(t *testing.T) {
	limit := NewLimiter(10)
	for i := 0; i < 1000; i++ {
		limit.Execute(func(param interface{}) {
			fmt.Println(i)
			time.Sleep(time.Second)
		}, "")
	}
	limit.Wait()
}

func TestLimit(t *testing.T) {
	LIMIT := 10
	N := 100

	c := NewLimiter(LIMIT)
	m := map[int]bool{}
	lock := &sync.Mutex{}

	max := int32(0)
	for i := 0; i < N; i++ {
		x := i
		c.Execute(func(x interface{}) {
			lock.Lock()
			m[x.(int)] = true
			currentMax := c.GetNumInProgress()
			if currentMax >= max {
				max = currentMax
			}
			lock.Unlock()
		}, x)
	}

	// wait until the above completes
	c.Wait()

	t.Log("results:", len(m))
	t.Log("max:", max)

	if len(m) != N {
		t.Error("invalid num of results", len(m))
	}

	if max > int32(LIMIT) || max == 0 {
		t.Error("invalid max", max)
	}
}

func TestNewConcurrencyLimiter(t *testing.T) {
	c := NewLimiter(0)
	if c.limit != DefaultLimit {
		t.Errorf("expected DefaultLimit: %d, got %d", c.limit, DefaultLimit)
	}

	LIMIT := DefaultLimit + (DefaultLimit / 2)
	c = NewLimiter(LIMIT)
	if cap(c.tickets) != LIMIT {
		t.Errorf("expected allocate the tickets %d, got %d", LIMIT, cap(c.tickets))
	}
}
