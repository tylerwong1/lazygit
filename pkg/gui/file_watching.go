package gui

import (
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/jesseduffield/lazygit/pkg/commands/models"
	"github.com/sirupsen/logrus"
)

type fileWatcher struct {
	Watcher          *fsnotify.Watcher
	WatchedFilenames []string
	Log              *logrus.Entry
	Disabled         bool

	// macs for some bizarre reason cap the number of watchable files to 256.
	// there's no obvious platform agonstic way to check the situation of the user's
	// computer so we're just arbitrarily capping at a small number in the default config.
	// This isn't so bad because file watching is only really an added bonus for faster refreshing.
	MaxWatchedFiles int
}

func NewFileWatcher(log *logrus.Entry, maxWatchedFiles int) *fileWatcher {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Error(err)
		return &fileWatcher{
			Disabled: true,
		}
	}

	return &fileWatcher{
		Watcher:          watcher,
		Log:              log,
		WatchedFilenames: make([]string, 0, maxWatchedFiles),
		MaxWatchedFiles:  maxWatchedFiles,
	}
}

func (w *fileWatcher) watchingFilename(filename string) bool {
	for _, watchedFilename := range w.WatchedFilenames {
		if watchedFilename == filename {
			return true
		}
	}
	return false
}

func (w *fileWatcher) popOldestFilename() {
	// shift the last off the array to make way for this one
	oldestFilename := w.WatchedFilenames[0]
	w.WatchedFilenames = w.WatchedFilenames[1:]
	if err := w.Watcher.Remove(oldestFilename); err != nil {
		// swallowing errors here because it doesn't really matter if we can't unwatch a file
		w.Log.Error(err)
	}
}

func (w *fileWatcher) watchFilename(filename string) {
	if err := w.Watcher.Add(filename); err != nil {
		// swallowing errors here because it doesn't really matter if we can't watch a file
		w.Log.Error(err)
	}

	// assume we're watching it now to be safe
	w.WatchedFilenames = append(w.WatchedFilenames, filename)
}

func (w *fileWatcher) addFilesToFileWatcher(files []*models.File) error {
	if w.Disabled {
		return nil
	}

	if len(files) == 0 {
		return nil
	}

	// watch the files for changes
	dirName, err := os.Getwd()
	if err != nil {
		return err
	}

	for _, file := range files[0:min(w.MaxWatchedFiles, len(files))] {
		if file.Deleted {
			continue
		}
		filename := filepath.Join(dirName, file.Name)
		if w.watchingFilename(filename) {
			continue
		}

		if len(w.WatchedFilenames) > w.MaxWatchedFiles {
			w.popOldestFilename()
		}

		w.watchFilename(filename)
	}

	return nil
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

// NOTE: given that we often edit files ourselves, this may make us end up refreshing files too often
// TODO: consider watching the whole directory recursively (could be more expensive)
func (gui *Gui) watchFilesForChanges() {
	gui.fileWatcher = NewFileWatcher(gui.Log, gui.maxWatchedFiles())
	if gui.fileWatcher.Disabled {
		return
	}
	go func() {
		for {
			select {
			// watch for events
			case event := <-gui.fileWatcher.Watcher.Events:
				if event.Op == fsnotify.Chmod {
					// for some reason we pick up chmod events when they don't actually happen
					continue
				}
				// only refresh if we're not already
				if !gui.State.IsRefreshingFiles {
					_ = gui.refreshSidePanels(refreshOptions{mode: ASYNC, scope: []int{FILES}})
				}

			// watch for errors
			case err := <-gui.fileWatcher.Watcher.Errors:
				if err != nil {
					gui.Log.Error(err)
				}
			}
		}
	}()
}

func (gui *Gui) maxWatchedFiles() int {
	return gui.Config.GetUserConfig().GetInt("MaxWatchedFiles")
}
