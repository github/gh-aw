//go:build !linux && !darwin

package main

func processRSSPeak() (*int64, error) { return nil, nil }
