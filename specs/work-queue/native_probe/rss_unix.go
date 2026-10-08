//go:build linux || darwin

package main

import (
	"runtime"
	"syscall"
)

func processRSSPeak() (*int64, error) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return nil, err
	}
	peak := usage.Maxrss
	if runtime.GOOS != "darwin" {
		peak *= 1024
	}
	return &peak, nil
}
