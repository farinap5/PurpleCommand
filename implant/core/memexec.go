package core

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

// memfdCreate creates an anonymous executable file in memory
func memfdCreate(name string) (int, error) {
	const SYS_MEMFD_CREATE = 319 // amd64
	const FLAGS = 0              // DO NOT use MFD_CLOEXEC

	namePtr, err := syscall.BytePtrFromString(name)
	if err != nil {
		return -1, err
	}

	fd, _, errno := syscall.Syscall(
		SYS_MEMFD_CREATE,
		uintptr(unsafe.Pointer(namePtr)),
		FLAGS,
		0,
	)

	if errno != 0 {
		return -1, fmt.Errorf("memfd_create failed: %v", errno)
	}

	return int(fd), nil
}

// ExecuteELFInMemory executes an ELF binary directly from RAM
func ExecuteELFInMemory(elfData []byte, args []string) (string, error) {

	// Create memfd
	fd, err := memfdCreate("payload")
	if err != nil {
		return "", err
	}

	defer syscall.Close(fd)

	// Write ELF bytes
	written := 0
	for written < len(elfData) {
		n, err := syscall.Write(fd, elfData[written:])
		if err != nil {
			return "", fmt.Errorf("write failed: %v", err)
		}
		written += n
	}

	// Make executable
	if err := syscall.Fchmod(fd, 0755); err != nil {
		return "", fmt.Errorf("chmod failed: %v", err)
	}

	// Reset file offset
	if _, err := syscall.Seek(fd, 0, 0); err != nil {
		return "", fmt.Errorf("seek failed: %v", err)
	}

	// Preserve fd in child process
	memFile := os.NewFile(uintptr(fd), "payload")

	// ExtraFiles starts at fd 3 in child
	execPath := "/proc/self/fd/3"

	cmd := exec.Command(execPath, args...)
	cmd.ExtraFiles = []*os.File{memFile}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n[stderr]:\n" + stderr.String()
	}

	if err != nil {
		return output, fmt.Errorf("execution failed: %v", err)
	}

	return output, nil
}