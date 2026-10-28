package collector

import (
	"sync"
	"testing"
)

func TestSeedRace(t *testing.T) {
	var x int
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); x++ }()
	}
	wg.Wait()
	_ = x
}
