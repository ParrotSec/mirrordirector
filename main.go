package main

import (
	"fmt"
	"time"

	"github.com/parrotsec/mirrordirector/files"
	"github.com/parrotsec/mirrordirector/mirrors"
	"github.com/parrotsec/mirrordirector/server"
)

func main() {
	time.Now().UTC().UnixNano()
	config := "/director/config/config.yaml"
	cache := "/director/data/cache.db"
	Root := mirrors.Init(config)
	Root.Show()

	Fileset := files.Init(Root, cache)

	go func(F *files.Fileset, R *mirrors.Root) {
		for {
			F.ScanMaster(*R)
			F.SaveCache(cache)
			R.Scan()
			R.Show()
			time.Sleep(time.Minute * 20)
		}
	}(&Fileset, &Root)

	fmt.Println("starting server")
	server.Serve(":8080", Fileset, Root)
}
