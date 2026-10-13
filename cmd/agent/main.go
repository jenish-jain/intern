package main

import (
	"os"

	logger "github.com/jenish-jain/logger"
)

func main() {
	logger.Init("debug")

	// cobra already prints the error as "Error: ...".
	if err := Execute(); err != nil {
		os.Exit(1)
	}
}
