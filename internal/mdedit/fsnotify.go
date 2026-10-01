package mdedit

import "github.com/fsnotify/fsnotify"

type fsnotifySource struct{ w *fsnotify.Watcher }

// newFsnotifySource watches dir. The directory rather than the file is
// watched because atomic saves (schmux's own, sed -i, most editors) replace
// the inode, and a watch on the old inode would go silent.
func newFsnotifySource(dir string) (EventSource, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := w.Add(dir); err != nil {
		_ = w.Close()
		return nil, err
	}
	return &fsnotifySource{w: w}, nil
}

func (s *fsnotifySource) Events() <-chan fsnotify.Event { return s.w.Events }
func (s *fsnotifySource) Errors() <-chan error          { return s.w.Errors }
func (s *fsnotifySource) Close() error                  { return s.w.Close() }
