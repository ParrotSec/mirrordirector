package main

import (
	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
	"io/fs"
	"os"
	"path/filepath"
)

var repository []string

func initWatcher() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	// recursive watch init
	err = filepath.Walk(config.repoPath, func(path string, info fs.FileInfo, err error) error {
		relPath, relErr := filepath.Rel(config.repoPath, path)
		if relErr != nil {
			return relErr
		}
		if relPath != "." {
			repository = append(repository, relPath)
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
			relPath, err := filepath.Rel(config.repoPath, event.Name)
			if err != nil {
				log.Fatal(err)
			}
			if event.Op & fsnotify.Create == fsnotify.Create {
				repository = append(repository, relPath)

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
				for i, p := range repository {
					if p == relPath {
						repository[i] = repository[len(repository) - 1]
						repository = repository[:len(repository) - 1]
						break
					}
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
