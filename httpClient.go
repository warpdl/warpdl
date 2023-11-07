package main

import "net/http"

func getHTTPClient() *http.Client {
	return &http.Client{
		Timeout: DEF_TIMEOUT,
	}
}
