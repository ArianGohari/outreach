package diagnostics

import (
	"encoding/json"
	"fmt"
	"github.com/gofrs/flock"
	"os"
	"path/filepath"
	"sync"
)

// A file lock also serializes diagnostics from an attempted second app instance.
type FileLog struct {
	path     string
	maxBytes int64
	backups  int
	mu       sync.Mutex
	lock     *flock.Flock
}

func OpenLog(directory string) (*FileLog, error) {
	if e := os.MkdirAll(directory, 0700); e != nil {
		return nil, e
	}
	path := filepath.Join(directory, "errors.jsonl")
	f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return nil, e
	}
	if e = f.Close(); e != nil {
		return nil, e
	}
	return &FileLog{path: path, maxBytes: 2 * 1024 * 1024, backups: 3, lock: flock.New(filepath.Join(directory, "errors.lock"))}, nil
}
func (l *FileLog) Path() string { return l.path }
func (l *FileLog) Append(p Problem) (err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err = l.lock.Lock(); err != nil {
		return err
	}
	defer func() {
		if e := l.lock.Unlock(); err == nil {
			err = e
		}
	}()
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if info, e := os.Stat(l.path); e == nil && info.Size()+int64(len(data)) > l.maxBytes {
		oldest := fmt.Sprintf("%s.%d", l.path, l.backups)
		if e = os.Remove(oldest); e != nil && !os.IsNotExist(e) {
			return e
		}
		for i := l.backups - 1; i >= 0; i-- {
			source := l.path
			if i > 0 {
				source = fmt.Sprintf("%s.%d", l.path, i)
			}
			target := fmt.Sprintf("%s.%d", l.path, i+1)
			if e = os.Rename(source, target); e != nil && !os.IsNotExist(e) {
				return e
			}
		}
	} else if e != nil && !os.IsNotExist(e) {
		return e
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if e := f.Close(); err == nil {
			err = e
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}
