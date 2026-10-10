//go:build !windows && !linux && !darwin

package sysmem

func total() uint64 { return 0 }
