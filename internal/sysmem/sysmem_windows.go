package sysmem

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// memoryStatusEx is Windows' MEMORYSTATUSEX.
type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

var globalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

func total() uint64 {
	m := memoryStatusEx{}
	m.length = uint32(unsafe.Sizeof(m))
	if ok, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m))); ok == 0 {
		return 0
	}
	return m.totalPhys
}
