package main

import (
	"fmt"
	"os"
	"strings"
)

type StreamLogger struct {
	indent string
}

func (sl *StreamLogger) LogFrame(remote bool, connID string, streamID uint32, format string, a ...interface{}) {
	var remoteStr string
	if remote {
		remoteStr = color("cyan", "=>")
	} else {
		remoteStr = color("magenta", "<=")
	}
	fmt.Fprintf(os.Stdout, "%s [%s] [%3d] %s\n", remoteStr, connID, streamID, fmt.Sprintf(format, a...))
}

func (sl *StreamLogger) LogFrameInfo(format string, a ...interface{}) {
	delimiter := color("gray", "|")
	fmt.Fprintf(os.Stdout, "%s%s %s\n", sl.indent, delimiter, fmt.Sprintf(format, a...))
}

func NewStreamLogger() *StreamLogger {
	logger := &StreamLogger{
		indent: strings.Repeat(" ", 15),
	}

	return logger
}

var logger = NewStreamLogger()

func color(color string, msg string) string {
	var code string

	switch color {
	case "red":
		code = "\x1b[31m"
	case "green":
		code = "\x1b[32m"
	case "yellow":
		code = "\x1b[33m"
	case "blue":
		code = "\x1b[34m"
	case "magenta":
		code = "\x1b[35m"
	case "cyan":
		code = "\x1b[36m"
	case "gray":
		code = "\x1b[90m"
	}

	return fmt.Sprintf("%s%s\x1b[0m", code, msg)
}
