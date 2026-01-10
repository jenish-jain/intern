package main

import (
	"os"

	logger "github.com/jenish-jain/logger"
)

func main() {
	logger.Init("debug")

	if err := Execute(); err != nil {
		logger.Error("Command failed: %v", err)
		os.Exit(1)
	}
}
