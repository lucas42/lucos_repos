package main

import (
	"errors"
	"log/slog"
	"net"
	"syscall"
)

// isClientDisconnect reports whether err is a client abandoning a response mid-write.
func isClientDisconnect(err error) bool {
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, net.ErrClosed)
}

// logResponseWriteError logs a failed response write: a client disconnect is
// normal and goes to Debug; anything else is a server fault and goes to Error.
func logResponseWriteError(msg string, err error) {
	if isClientDisconnect(err) {
		slog.Debug(msg+": client disconnected", "error", err)
		return
	}
	slog.Error(msg, "error", err)
}
