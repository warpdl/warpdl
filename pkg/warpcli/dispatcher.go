package warpcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/warpdl/warpdl/common"
)

type Dispatcher struct {
	Handlers map[common.UpdateType]Handler
	mu       sync.RWMutex
}

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
	if h, ok := d.Handlers[res.Update.Type]; ok {
		return h.Handle(res.Update.Message)
	}
	// return fmt.Errorf("no handler for update (type=%s): %s", res.Update.Type, string(res.Update.Message))
	return nil
}

func (d *Dispatcher) AddHandler(t common.UpdateType, h Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Handlers[t] = h
}

func (d *Dispatcher) RemoveHandler(t common.UpdateType) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.Handlers, t)
}
