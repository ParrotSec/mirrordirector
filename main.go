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
	config := "config.yaml"
	cache := "cache.db"
	Root := mirrors.Init(config)
	Root.Show()

	Fileset := files.Init(Root, cache)

	go func(F *files.Fileset, R *mirrors.Root) {
		for {
<<<<<<< HEAD
			F.ScanMaster(Root)
=======
			F.ScanMaster(*R)
>>>>>>> fa9c1a66807b71864f1f5ee9d42a0053f1d3f5b9
			F.SaveCache(cache)
			R.Scan()
			R.Show()
			time.Sleep(time.Minute * 20)
		}
	}(&Fileset, &Root)

	fmt.Println("starting server")
	server.Serve(":8080", Fileset, Root)
}
