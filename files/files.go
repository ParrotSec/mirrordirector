package files

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/parrotsec/mirrordirector/mirrors"
)

type File struct {
	Uri     string
	Version uint64
}

type Fileset struct {
	Files map[string]File `yaml:",inline"`
}

func Init(R mirrors.Root, cache string) Fileset {
	var F Fileset
	F.Files = make(map[string]File, 500)
	F.LoadCache(cache)
	F.ScanMaster(R)
	F.SaveCache(cache)

	return F
}

func (F Fileset) Show() {
	for _, ii := range F.Files {
		fmt.Printf("%s %d\n", ii.Uri, ii.Version)
	}
}

func (f Fileset) Lookup(file string) (File, error) {
	if ii, ok := f.Files[file]; ok {
		return ii, nil
	}
	return File{}, fmt.Errorf("file not found: %s", file)
}

func (F *Fileset) ScanMaster(R mirrors.Root) error {
	// download index
	resp, err := http.Get(R.Continents["MASTER"].Countries["MASTER"].Mirrors[0].Url + "/index.db")
	if err != nil {
		log.Printf("Unable to get index from master repo: %v\n", err)
		return fmt.Errorf("unable to get index from master repo: %v", err)
	}
	defer resp.Body.Close()

	// read body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Unable to get index from master repo: %v\n", err)
		return fmt.Errorf("unable to get index from master repo: %v", err)
	}

	// extract index version
	if !strings.Contains(string(body), "!version") {
		log.Printf("Got invalid index from master repo: version string not found\n")
		return fmt.Errorf("got invalid index from master repo: version string not found")
	}

	// parse files available
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	version, err := strconv.ParseUint(strings.Split(lines[0], " ")[1], 10, 64)
	if err != nil {
		log.Printf("Got invalid version from index while scanning master repo\n")
		return fmt.Errorf("got invalid version from index while scanning master repo")
	}

	// save files and versions in F.Files structure
	for _, ii := range lines[1:] {
		if _, ok := F.Files[ii]; !ok {
			F.Files[ii] = File{Uri: ii, Version: version}
		}
	}

	return nil
}

func (F *Fileset) LoadCache(fname string) error {
	if _, err := os.Stat(fname); os.IsNotExist(err) {
		return fmt.Errorf("skipping load from cache: file does not exist")
	}
	f, err := ioutil.ReadFile(fname)
	if err != nil {
		log.Printf("Can't open cache file %v\n", err)
		return fmt.Errorf("can't open cache file: %v", err)
	}
	for _, ii := range strings.Split(strings.TrimSpace(string(f)), "\n") {
		version, _ := strconv.ParseUint(strings.Split(ii, "|")[0], 10, 64)
		uri := strings.Split(ii, "|")[1]
		F.Files[uri] = File{Version: version, Uri: uri}
	}

	return nil
}

func (F Fileset) SaveCache(fname string) error {
	f, err := os.Create(fname)
	if err != nil {
		log.Fatalf("Unable to create cache file: %v\n", err)
		return fmt.Errorf("unable to create cache file: %v", err)
	}
	w := bufio.NewWriter(f)
	for _, ii := range F.Files {
		w.WriteString(fmt.Sprint(ii.Version) + "|" + ii.Uri + "\n")
	}
	w.Flush()
	defer f.Close()

	return nil
}
