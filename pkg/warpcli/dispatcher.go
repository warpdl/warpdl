package warpcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/warpdl/warpdl/common"
)

// Dispatcher routes incoming daemon updates to their registered handlers.
// It maintains a thread-safe map of handlers keyed by update type.
type Dispatcher struct {
	Handlers map[common.UpdateType][]Handler
	mu       sync.RWMutex
}

// ErrDisconnect is a sentinel error returned by handlers to signal that
// the client should disconnect from the daemon.
var ErrDisconnect error = errors.New("disconnect")

func (d *Dispatcher) process(buf []byte) error {
	var res Response
	err := json.Unmarshal(buf, &res)
	if err != nil {
		return fmt.Errorf("failed to parse (%s): '%s'", err.Error(), string(buf))
	}
	if !res.Ok {
		return errors.New(res.Error)
	}
	d.mu.RLock()
	handlers, ok := d.Handlers[res.Update.Type]
	d.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no handler for update (type=%s): %s", res.Update.Type, string(res.Update.Message))
	}
	for _, h := range handlers {
		err = h.Handle(res.Update.Message)
		if err != nil {
			return err
		}
	}
	// return fmt.Errorf("no handler for update (type=%s): %s", res.Update.Type, string(res.Update.Message))
	return nil
}

// AddHandler registers a handler for the specified update type.
// Multiple handlers can be registered for the same update type and will
// be called in the order they were added when an update of that type arrives.
func (d *Dispatcher) AddHandler(t common.UpdateType, h Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Handlers[t] = append(d.Handlers[t], h)
}

// RemoveHandler removes all handlers registered for the specified update type.
func (d *Dispatcher) RemoveHandler(t common.UpdateType) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.Handlers, t)
}
