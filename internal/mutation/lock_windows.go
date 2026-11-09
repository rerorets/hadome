package mutation

import (
	"errors"
	"os"
	"syscall"
)

// lock takes the lock file at path, or fails at once when another process holds it. The file is
// opened so that no one else can open it for writing; Windows closes the handle when the process
// ends, however it ends. Others may still read it, to learn which process holds it.
func lock(path string) (release func(), err error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ, nil,
		syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, errSharingViolation) {
			return nil, held(path)
		}
		return nil, err
	}
	f := os.NewFile(uintptr(h), path)
	if err := writePid(f); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

// errSharingViolation is ERROR_SHARING_VIOLATION: another handle opened the file without sharing it.
const errSharingViolation syscall.Errno = 32
