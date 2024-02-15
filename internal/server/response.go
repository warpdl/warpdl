package server

import "encoding/json"

type Response struct {
	Ok      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Message any    `json:"message,omitempty"`
}

func MakeResult(res any) []byte {
	b, _ := json.Marshal(Response{
		Ok:      true,
		Message: res,
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
