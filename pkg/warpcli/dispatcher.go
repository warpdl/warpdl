package warpcli

import (
	"encoding/json"
	"errors"
	"fmt"
)

type Dispatcher struct {
	Handlers map[string]Handler
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
		return h.Callback(res.Update.Message)
	}
	fmt.Println(string(res.Update.Message))
	return nil
}
