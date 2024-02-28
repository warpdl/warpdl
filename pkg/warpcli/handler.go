package warpcli

import "encoding/json"

type Handler interface {
	Callback(json.RawMessage) error
}
