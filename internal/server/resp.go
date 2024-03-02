package server

import "encoding/json"

type Response struct {
	Ok     bool    `json:"ok"`
	Error  string  `json:"error,omitempty"`
	Update *Update `json:"update,omitempty"`
}

type Update struct {
	Type    string `json:"type"`
	Message any    `json:"message,omitempty"`
}

func MakeResult(utype string, res any) []byte {
	b, _ := json.Marshal(Response{
		Ok: true,
		Update: &Update{
			Type:    utype,
			Message: res,
		},
	})
	return b
}

func InitError(err error) []byte {
	if err == nil {
		return CreateError("Unknown")
	}
	return CreateError(err.Error())
}

func CreateError(err string) []byte {
	b, _ := json.Marshal(Response{
		Ok:    false,
		Error: err,
	})
	return b
}
