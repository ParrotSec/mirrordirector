package mirrors

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// client is used to scan the mirrors and fetch the remote config:
// it must have a timeout, a stuck mirror would otherwise stall the
// whole rescan loop forever
var client = &http.Client{Timeout: time.Second * 10}

type Mirror struct {
	Name string `yaml:"name"`
	Url  string `yaml:"url"`
	//	Index            string   `yaml:"index"`
	BlockedCountries []string `yaml:"blocked_countries"`
	Down             bool     `yaml:"down"`
	Version          uint64   `yaml:"version"`
}

type Country struct {
	Mirrors []Mirror `yaml:"mirrors"`
}

type Continent struct {
	Countries map[string]Country `yaml:",inline"`
}

type Root struct {
	Continents map[string]Continent `yaml:"continents"`
}

func Init(config string) Root {
	var R Root
	var mraw []byte
	var err error

	if strings.HasPrefix(config, "http://") || strings.HasPrefix(config, "https://") {
		resp, err := client.Get(config)
		if err != nil {
			log.Fatalf("fetching %s error: %v", config, err)
		}
		defer resp.Body.Close()
		mraw, err = io.ReadAll(resp.Body)
		if err != nil {
			log.Fatalf("reading %s response error: %v", config, err)
		}
	} else {
		mraw, err = os.ReadFile(config)
		if err != nil {
			log.Fatalf("%s file missing", config)
		}
	}

	err = yaml.Unmarshal(mraw, &R)
	if err != nil {
		log.Fatalf("parsing %s error: %v", config, err)
	}

	return R
}

// DeepCopy returns an independent copy of the mirror tree: the maps
// and the Mirrors slices are duplicated, so the copy can be mutated
// (e.g. by Reload or Scan) without affecting the original. This
// allows the scanner to work on a private copy and publish it
// copy-on-write, with no locking on the read path.
func (R Root) DeepCopy() Root {
	out := Root{Continents: make(map[string]Continent, len(R.Continents))}
	for ctName, ct := range R.Continents {
		newCt := Continent{Countries: make(map[string]Country, len(ct.Countries))}
		for coName, co := range ct.Countries {
			newCo := Country{Mirrors: make([]Mirror, len(co.Mirrors))}
			copy(newCo.Mirrors, co.Mirrors)
			newCt.Countries[coName] = newCo
		}
		out.Continents[ctName] = newCt
	}
	return out
}

func (R *Root) Reload(config string) {
	var mraw []byte
	var err error

	if strings.HasPrefix(config, "http://") || strings.HasPrefix(config, "https://") {
		resp, err := client.Get(config)
		if err != nil {
			log.Printf("fetching %s error: %v", config, err)
			return
		}
		defer resp.Body.Close()
		mraw, err = io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("reading %s response error: %v", config, err)
			return
		}
	} else {
		mraw, err = os.ReadFile(config)
		if err != nil {
			log.Printf("%s file missing", config)
			return
		}
	}

	// parse into a fresh tree and swap it in only on success: a
	// failed reload keeps the previous mirror list, and removed
	// mirrors don't linger from a merge with the old maps
	var newR Root
	err = yaml.Unmarshal(mraw, &newR)
	if err != nil {
		log.Printf("parsing %s error: %v", config, err)
		return
	}
	*R = newR
}

func (R Root) Show() {
	for k, e := range R.Continents {
		for l, f := range e.Countries {
			for _, g := range f.Mirrors {
				log.Printf("%s/%s/%s: %s version: %d, down: %v\n", k, l, g.Name, g.Url, g.Version, g.Down)
			}
		}
	}
}

// pickHealthy returns a randomly chosen mirror that is up and fresh
// enough (version >= the requested file's version). Mirrors are
// tried in random order without replacement, so a list is given up
// only when ALL of its mirrors are down or stale. The bool is false
// when no mirror is usable (or the list is empty).
//
// TODO: find a way to weight mirror redirection based on available
//       bandwidth
func pickHealthy(mirrors []Mirror, version uint64) (Mirror, bool) {
	for _, i := range rand.Perm(len(mirrors)) {
		if m := mirrors[i]; !m.Down && m.Version >= version {
			return m, true
		}
	}
	return Mirror{}, false
}

func (R Root) Lookup(file string, version uint64, continent string, country string) Mirror {
	if ct, ctok := R.Continents[continent]; ctok {
		// prefer a mirror in the client's own country
		if co, cook := ct.Countries[country]; cook {
			if mirror, ok := pickHealthy(co.Mirrors, version); ok {
				return mirror
			}
		}
		// fall back to the continent DEFAULT mirrors
		if co, cook := ct.Countries["DEFAULT"]; cook {
			if mirror, ok := pickHealthy(co.Mirrors, version); ok {
				return mirror
			}
		}
	}
	// fall back to the global DEFAULT mirrors
	if ct, ctok := R.Continents["DEFAULT"]; ctok {
		if co, cook := ct.Countries["DEFAULT"]; cook {
			if mirror, ok := pickHealthy(co.Mirrors, version); ok {
				return mirror
			}
		}
	}
	// last resort: the master infrastructure, always served
	// regardless of down/version state
	return R.Continents["MASTER"].Countries["MASTER"].Mirrors[rand.Intn(
		len(R.Continents["MASTER"].Countries["MASTER"].Mirrors),
	)]
}

func (R *Root) Scan() {
	for continentName, continent := range R.Continents {
		for countryName, country := range continent.Countries {
			for mcount, mirror := range country.Mirrors {
				log.Printf("[SCANNING] %s/%s/%s: %s version: %d, down: %v\n", continentName, countryName, mirror.Name, mirror.Url, mirror.Version, mirror.Down)
				R.Continents[continentName].Countries[countryName].Mirrors[mcount].Scan()
			}
		}
	}
}

func (M *Mirror) Scan() error {
	resp, err := client.Get(M.Url + "/misc/vindex.db")
	if err != nil {
		log.Printf("[WARNING] Unable to get index from %s: %v\n", M.Name, err)
		M.Down = true
		return fmt.Errorf("unable to get index from %s: %v", M.Name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[WARNING] Unable to get index from %s: %v", M.Name, err)
		M.Down = true
		return fmt.Errorf("unable to get index from %s: %v", M.Name, err)
	}

	if !strings.Contains(string(body), "!version") {
		log.Printf("[WARNING] Got invalid index from %s: version string not found\n", M.Name)
		M.Down = true
		return fmt.Errorf("got invalid index from %s: version string not found", M.Name)
	}

	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	version, err := strconv.ParseUint(strings.Split(lines[0], " ")[1], 10, 64)
	if err != nil {
		log.Printf("[WARNING] Got invalid version from index while scanning %s\n", M.Name)
		M.Down = true
		return fmt.Errorf("got invalid version from index while scanning %s", M.Name)
	}

	M.Version = version
	M.Down = false

	return nil
}
