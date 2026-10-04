//go:build !linux

package testtmpfs

func run(fn func() int) int { return fn() }
