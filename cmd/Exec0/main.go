package main

import (
	"github.com/aarctanz/Exec0/internal/config"
)

func main() {
	_, err := config.LoadConfig()
	if err != nil {
		panic("failed to load config: " + err.Error())
	}
}
