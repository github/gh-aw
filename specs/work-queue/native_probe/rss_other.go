//go:build !linux && !darwin

package main

func processRSSPeak() int64 { return 0 }
