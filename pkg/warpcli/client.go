package warpcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/warpdl/warpdl/common"
)

type Client struct {
	mu   *sync.RWMutex
	d    *Dispatcher
	conn net.Conn
}

func NewClient() (*Client, error) {
	socketPath := filepath.Join(os.TempDir(), "warpdl.sock")
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		err = fmt.Errorf("error connecting to server: %s", err.Error())
		return nil, err
	}
	return &Client{
		conn: conn,
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{},
	}, nil
}

func (c *Client) Listen() (err error) {
	defer c.conn.Close()
	for {
		c.mu.RLock()
		var buf []byte
		buf, err = read(c.conn)
		if err != nil {
			c.mu.RUnlock()
			err = fmt.Errorf("error reading: %s", err.Error())
			return
		}
		err = c.d.process(buf)
		if err != nil {
			c.mu.RUnlock()
			if err == ErrDisconnect {
				break
			}
			err = fmt.Errorf("error processing: %s", err.Error())
			return
		}
		c.mu.RUnlock()
	}
	return
}

func (c *Client) invoke(method common.UpdateType, message any) (json.RawMessage, error) {
	// block updates listener while invoking a method
	// to retrieve the message update here instead
	c.mu.Lock()
	defer c.mu.Unlock()
	buf, err := json.Marshal(&Request{
		Method:  method,
		Message: message,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to invoke %s: %s", method, err.Error())
	}
	err = write(c.conn, buf)
	if err != nil {
		return nil, fmt.Errorf("failed to invoke %s: %s", method, err.Error())
	}
	buf, err = read(c.conn)
	if err != nil {
		return nil, fmt.Errorf("failed to invoke %s: %s", method, err.Error())
	}
	var res Response
	err = json.Unmarshal(buf, &res)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %s", method, err.Error())
	}
	if !res.Ok {
		return nil, errors.New(res.Error)
	}
	return res.Update.Message, nil
}
