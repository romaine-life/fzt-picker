package picker

import "syscall"

func isHiddenFile(path string) bool {
	pathW, _ := syscall.UTF16PtrFromString(path)
	attrs, err := syscall.GetFileAttributes(pathW)
	if err != nil {
		return true // can't read attributes — likely a protected system folder
	}
	const FILE_ATTRIBUTE_HIDDEN = 0x2
	const FILE_ATTRIBUTE_SYSTEM = 0x4
	return attrs&(FILE_ATTRIBUTE_HIDDEN|FILE_ATTRIBUTE_SYSTEM) != 0
}
