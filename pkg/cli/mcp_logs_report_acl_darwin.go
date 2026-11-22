package cli

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"unsafe"
)

func validateMCPLogsDirectoryACL(root *os.Root, path string, expected fs.FileInfo) error {
	var dir *os.File
	var err error
	flags := os.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW
	if root == nil {
		dir, err = os.OpenFile(path, flags, 0)
	} else {
		dir, err = root.OpenFile(path, flags, 0)
	}
	if err != nil {
		return fmt.Errorf("open logs cache directory %q for ACL check: %w", path, err)
	}
	defer func() {
		if err := dir.Close(); err != nil {
			mcpLogsGuardrailLog.Printf("Failed to close logs cache directory: %v", err)
		}
	}()
	info, err := dir.Stat()
	if err != nil {
		return fmt.Errorf("inspect logs cache directory %q for ACL check: %w", path, err)
	}
	currentStat, currentOK := info.Sys().(*syscall.Stat_t)
	expectedStat, expectedOK := expected.Sys().(*syscall.Stat_t)
	if !currentOK || !expectedOK || !os.SameFile(expected, info) || info.Mode() != expected.Mode() || currentStat.Uid != expectedStat.Uid {
		return fmt.Errorf("logs cache directory %q changed during its ACL check", path)
	}
	if err := validateMCPLogsACL(dir); err != nil {
		return fmt.Errorf("logs cache directory %q requires no extended ACL: %w", path, err)
	}
	return nil
}

func validateMCPLogsACL(file *os.File) error {
	// fgetattrlist(2) returns a length followed by an attrreference_t.
	// Request the full size so an ACL payload that cannot fit is never
	// mistaken for an absent ACL. This works without cgo.
	const (
		attrCommonExtendedSecurity = 0x00400000
		fsoptReportFullSize        = 0x00000004
	)
	// Darwin's struct attrlist has two 16-bit fields and five 32-bit bitmaps.
	attrs := struct {
		count, reserved                 uint16
		common, volume, dir, file, fork uint32
	}{count: 5, common: attrCommonExtendedSecurity}
	var buf [12]byte
	raw, err := file.SyscallConn()
	if err != nil {
		return fmt.Errorf("access logs cache descriptor for ACL check: %w", err)
	}
	var errno syscall.Errno
	err = raw.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall6(syscall.SYS_FGETATTRLIST, fd, uintptr(unsafe.Pointer(&attrs)), uintptr(unsafe.Pointer(&buf)), uintptr(len(buf)), fsoptReportFullSize, 0)
	})
	if err != nil {
		return fmt.Errorf("inspect logs cache ACL: %w", err)
	}
	if errno != 0 {
		return fmt.Errorf("inspect logs cache ACL: %w", errno)
	}
	if binary.NativeEndian.Uint32(buf[:4]) != uint32(len(buf)) || binary.NativeEndian.Uint32(buf[8:]) != 0 {
		return errors.New("logs cache entry has an extended ACL or its absence could not be verified")
	}
	return nil
}
