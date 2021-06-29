package watcher

import (
	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
	"io/fs"
	"os"
	"parrot-redirector/db"
	"parrot-redirector/handlers"
	"path/filepath"
)

func InitWatcher(repoPath string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	// recursive watch init
	err = filepath.Walk(repoPath, func(path string, info fs.FileInfo, err error) error {
		relPath, relErr := filepath.Rel(repoPath, path)
		if relErr != nil {
			return relErr
		}
		if err != nil {
			log.Errorf("prevent panic by handling failure accessing a path %q: %v\n", path, err)
			return err
		}
		if info.IsDir() {
			err := watcher.Add(path)
			if err != nil {
				log.Error(err)
			}
		} else {
			db.Cache.AddFile(relPath)
		}
		log.Infof("visited file or dir: %q\n", path)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			log.Println("event:", event)
			relPath, err := filepath.Rel(repoPath, event.Name)
			if err != nil {
				log.Fatal(err)
			}
			if event.Op & fsnotify.Create == fsnotify.Create {
				db.Cache.AddFile(relPath)
				for countryCode := range handlers.SyncPoint {
					handlers.SyncPoint[countryCode][relPath] = false
				}
				if info, err := os.Stat(event.Name); err == nil {
					if info.IsDir() {
						if err := watcher.Add(event.Name); err != nil {
							log.Error(err)
						}
					}
				} else {
					log.Error(err)
				}
			}
			if event.Op & fsnotify.Remove == fsnotify.Remove {
				db.Cache.RemoveFile(relPath)
				for countryCode := range handlers.SyncPoint {
					delete(handlers.SyncPoint[countryCode], relPath)
				}
			}
			if event.Op & fsnotify.Write == fsnotify.Write {
				log.Println("modified file:", event.Name)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Error( err)
		}
	}
}
