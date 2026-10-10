// Package sysmem reports how much memory the machine has, so caches can
// be sized to it.
package sysmem

// Total is the machine's physical memory in bytes, or 0 when unknown.
func Total() uint64 { return total() }
