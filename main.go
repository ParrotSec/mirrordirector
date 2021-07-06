package main

import (
	"time"

	"github.com/parrotsec/mirrordirector/files"
	"github.com/parrotsec/mirrordirector/mirrors"
	"github.com/parrotsec/mirrordirector/server"
)

func main() {
	time.Now().UTC().UnixNano()
	config := "config.yaml"
	cache := "cache.db"
	Root := mirrors.Init(config)
	Root.Show()
	Root.Scan()
	Root.Show()

	Fileset := files.Init(Root, cache)
	Fileset.ScanMaster(Root)
	Fileset.SaveCache(cache)

	server.Serve(":8080", Fileset, Root)
}
