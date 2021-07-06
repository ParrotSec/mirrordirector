package mirrors

import (
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Mirror struct {
	Name             string   `yaml:"name"`
	Url              string   `yaml:"url"`
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

	mraw, err := os.ReadFile(config)
	if err != nil {
		log.Fatalf("%s file missing", config)
	}

	err = yaml.Unmarshal(mraw, &R)
	if err != nil {
		log.Fatalf("parsing %s error: %v", config, err)
	}

	return R
}

func (R Root) Show() {
	for k, e := range R.Continents {
		for l, f := range e.Countries {
			for _, g := range f.Mirrors {
				fmt.Printf("%s/%s/%s: %s version: %d, down: %v\n", k, l, g.Name, g.Url, g.Version, g.Down)
			}
		}
	}
}

func (R Root) Lookup(file string, version uint64, continent string, country string) Mirror {
	fmt.Printf("Lookup( %s, %d, %s, %s )\n", file, version, continent, country)
	if ct, ctok := R.Continents[continent]; ctok {
		if co, cook := ct.Countries[country]; cook {
			// if country has a mirror
			mirror := co.Mirrors[rand.Intn(len(co.Mirrors))]
			if !mirror.Down {
				if mirror.Version >= version {
					return mirror
				}
			}
		}
		if co, cook := ct.Countries["DEFAULT"]; cook {
			// if continent has a DEFAULT set of mirrors
			mirror := co.Mirrors[rand.Intn(len(co.Mirrors))]
			if !mirror.Down {
				if mirror.Version >= version {
					return mirror
				}
			}
		}
		// if country has no mirrors and continent has no DEFAULT mirrors
		for _, co := range ct.Countries {
			// pick a random mirror casually from an available country
			fmt.Println(co.Mirrors)
			mirror := co.Mirrors[rand.Intn(len(co.Mirrors))]
			if !mirror.Down {
				if mirror.Version >= version {
					return mirror
				}
			}
		}
	}
	// if there are no mirrors in the continent
	if ct, ctok := R.Continents["DEFAULT"]; ctok {
		if co, cook := ct.Countries["DEFAULT"]; cook {
			mirror := co.Mirrors[rand.Intn(len(co.Mirrors))]
			if !mirror.Down {
				if mirror.Version >= version {
					return mirror
				}
			}
		}
	}
	// if nothing is available
	return R.Continents["MASTER"].Countries["MASTER"].Mirrors[rand.Intn(
		len(R.Continents["MASTER"].Countries["MASTER"].Mirrors),
	)]
}

func (R *Root) Scan() error {
	for continentName, continent := range R.Continents {
		for countryName, country := range continent.Countries {
			for mcount, mirror := range country.Mirrors {
				log.Printf("Scanning %s [%s/%s}", mirror.Name, continentName, countryName)
				err := R.Continents[continentName].Countries[countryName].Mirrors[mcount].Scan()
				if err != nil {
					log.Printf("%s [%s/%s} scan failed: %v\n", mirror.Name, continentName, countryName, err)
				}
			}
		}
	}
	return nil
}

func (M *Mirror) Scan() error {
	resp, err := http.Get(M.Url + "/index.db")
	if err != nil {
		log.Printf("Unable to get index from %s: %v\n", M.Name, err)
		M.Down = true
		return fmt.Errorf("unable to get index from %s: %v", M.Name, err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Unable to get index from %s: %v", M.Name, err)
		M.Down = true
		return fmt.Errorf("unable to get index from %s: %v", M.Name, err)
	}

	if !strings.Contains(string(body), "!version") {
		log.Printf("Got invalid index from %s: version string not found\n", M.Name)
		M.Down = true
		return fmt.Errorf("got invalid index from %s: version string not found", M.Name)
	}

	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	version, err := strconv.ParseUint(strings.Split(lines[0], " ")[1], 10, 64)
	if err != nil {
		log.Printf("Got invalid version from index while scanning %s\n", M.Name)
		M.Down = true
		return fmt.Errorf("got invalid version from index while scanning %s", M.Name)
	}

	M.Version = version
	M.Down = false

	return nil
}
