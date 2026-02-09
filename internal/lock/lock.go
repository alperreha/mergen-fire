package lock

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

var ErrAlreadyLocked = errors.New("lock already held")

type FileLock struct {
	file *os.File
}

func Acquire(path string) (*FileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}

	lockFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o640)
	if err != nil {
		return nil, err
	}

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lockFile.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrAlreadyLocked
		}
		return nil, err
	}

	return &FileLock{file: lockFile}, nil
}

func (l *FileLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}

	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
		_ = l.file.Close()
		return err
	}
	return l.file.Close()
}
