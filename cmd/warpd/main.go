package main

import (
	"fmt"
	"log"
	"os"

	"github.com/warpdl/warpdl/internal/api"
	"github.com/warpdl/warpdl/internal/server"
)

func main() {
	s, err := api.NewApi(log.Default())
	if err != nil {
		fmt.Println("warpd:", err.Error())
		os.Exit(1)
	}
	serv := server.NewServer(log.Default())
	s.RegisterHandlers(serv)
	err = serv.Start()
	if err != nil {
		fmt.Println("warpd:", err.Error())
		os.Exit(1)
	}
}
