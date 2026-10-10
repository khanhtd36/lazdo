package sysmem

import "syscall"

func total() uint64 {
	var info syscall.Sysinfo_t
	if syscall.Sysinfo(&info) != nil {
		return 0
	}
	return uint64(info.Totalram) * uint64(info.Unit) //nolint:unconvert // uint32 on 32-bit systems
}
