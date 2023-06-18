package main

import (
	"fmt"
	"strings"
)

type (
	confirmAction interface {
		action() string
	}
	command string
)

func (a command) action() string {
	return strings.Join([]string{string(a), "command"}, " ")
}

func confirm(c confirmAction, force ...bool) bool {
	if len(force) != 0 && force[0] {
		return true
	}
	fmt.Printf("Are you sure you want to proceed with the %s? (yes/no): ", c.action())
	var i string
	_, _ = fmt.Scanf("%s", &i)
	i = strings.ToLower(i)
	switch i {
	case "yes", "y", "true", "1":
		return true
	default:
		fmt.Printf("Cancelled %s operation!\n", c)
		return false
	}
}
