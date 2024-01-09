package waitgroup

import (
	"golang.org/x/exp/maps"
	"log"
	"sync"
	"time"
)

type WG struct {
	logger *log.Logger
	wg     *sync.WaitGroup
	groups map[string]struct{}
	mu     *sync.RWMutex // Changed to RWMutex for better read performance
}

func NewWaitGroup(logger *log.Logger) *WG {
	return &WG{
		logger: logger,
		wg:     &sync.WaitGroup{},
		groups: make(map[string]struct{}),
		mu:     &sync.RWMutex{},
	}
}

func (w *WG) Add(i int, msg string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.wg.Add(i)
	w.groups[msg] = struct{}{}
	w.log("Add", msg)
}

func (w *WG) Done(msg string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.groups[msg]; !exists {
		w.logger.Printf("[%s] Attempt to mark non-existent group as done: %s\n", time.Now().Format(time.RFC3339), msg, maps.Keys(w.groups))
		return
	}

	delete(w.groups, msg)
	w.wg.Done()
	w.log("Done", msg)
}

func (w *WG) Groups() map[string]struct{} {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Returning a copy of the map for safety
	copiedMap := make(map[string]struct{})
	for k, v := range w.groups {
		copiedMap[k] = v
	}
	return copiedMap
}

func (w *WG) Wait() {
	w.wg.Wait()
}

// log is a helper method for logging group changes.
func (w *WG) log(action, msg string) {
	keys := maps.Keys(w.groups)
	w.logger.Printf("[%s] %s: %s, groups size: %d, keys: %+v\n", time.Now().Format(time.RFC3339), action, msg, len(w.groups), keys)
}
