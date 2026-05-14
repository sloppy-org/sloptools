package main

import (
	"fmt"
	"os"
	"strings"
)

func nightLog(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "brain night: "+format+"\n", args...)
}

func stageLabel(stage string) string {
	if strings.TrimSpace(stage) == "" {
		return "all"
	}
	return stage
}
