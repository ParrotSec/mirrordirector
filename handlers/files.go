package handlers

import (
	"errors"
	"github.com/gorilla/mux"
	"github.com/oschwald/geoip2-golang"
	log "github.com/sirupsen/logrus"
	"net"
	"net/http"
	"net/url"
	"parrot-redirector/types"
)

// returns first mirror link which satisfies file request
func fileCheck(mirrors []types.Mirror, filePath string, userCountryCode string) (string, error) {
	for _, value := range mirrors {
		var blocked bool

		for _, blockedCountryCode := range value.BlockedCountries {
			if blockedCountryCode == userCountryCode {
				blocked = true
				break
			}
		}

		if blocked {
			continue
		}

		base, err := url.Parse(value.Url)
		if err != nil {
			log.Errorf("error parsing base URL: %v", err)
			continue
		}

		postfix, err := url.Parse(base.Path + "/" + filePath)
		if err != nil {
			log.Errorf("error parsing postfix URL: %v", err)
			continue
		}

		fileLink := base.ResolveReference(postfix).String()
		resp, err := http.Head(fileLink)
		if err != nil {
			log.Errorf("error initiating HEAD request: %v", err)
			// mirror is down
			value.Down = true
			continue
		}

		if resp.StatusCode == http.StatusOK {
			return fileLink, nil
		}
	}
	return "", errors.New("file wasn't found")
}

// Route which redirects to a closest mirror
func NewFilesHandler(m types.MirrorsYAML, filesList []string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		// performing a sync health check sorted by speed
		db, err := geoip2.Open("country.mmdb")
		if err != nil {
			log.Errorf("Error opening geolite db")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		host, _, err := net.SplitHostPort( /*r.RemoteAddr*/ "159.224.217.17:1111")
		if err != nil {
			log.Errorf("Error splitting ip and port")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		dbResult, err := db.Country(net.ParseIP(host))
		if err != nil {
			log.Errorf("Error getting country from geodb")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		userCountry := dbResult.Country.IsoCode
		userContinent := dbResult.Continent.Code
		vars := mux.Vars(r)

		// checking if file should be in the repo
		found := false
		for _, f := range filesList {
			if f == vars["filePath"] {
				found = true
				break
			}
		}
		log.Print(dbResult.Continent.Code)

		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var finalLink string

		// Validation if user's continent and country exists in defined mirrors file
		// have to do it over index to keep the same address to be able to mark mirror as down
		if _, continentExists := m.Continents[userContinent]; continentExists {
			if _, countryExists := m.Continents[userContinent].Countries[userCountry]; countryExists {
				if finalLink, err = fileCheck(m.Continents[userContinent].Countries[userCountry].Mirrors[:], vars["filePath"], userCountry); err != nil {
					// fallback to other countries
					// looking for random correct mirror in continent
					for countryCode, _ := range m.Continents[userContinent].Countries {
						if userCountry == countryCode {
							continue
						}

						if finalLink, err = fileCheck(m.Continents[userContinent].Countries[countryCode].Mirrors[:], vars["filePath"], userCountry); err != nil {
							break
						}
					}

					// if it's still empty we need to fallback to continents
					if finalLink == "" {
						for continentCode := range m.Continents {
							if userContinent == continentCode {
								continue
							}

							for countryCode := range m.Continents[continentCode].Countries {
								if finalLink, err = fileCheck(m.Continents[continentCode].Countries[countryCode].Mirrors[:], vars["filePath"], userCountry); err == nil {
									break
								}
							}

							if finalLink != "" {
								break
							}
						}
					}

					if finalLink == "" {
						// TODO: redirect do other CDN
						log.Warnf("user %s:%s could not find a file %s on any mirror", userContinent, userCountry, vars["filePath"])
						w.WriteHeader(http.StatusNotFound)
						return
					}
				}
			} else {
				// fallback to other continents, choosing random country
				for countryCode, _ := range m.Continents[userContinent].Countries {
					if finalLink, err = fileCheck(m.Continents[userContinent].Countries[countryCode].Mirrors[:], vars["filePath"], userCountry); err == nil {
						break
					}
				}

				// fallback to other continents
				if finalLink == "" {
					for continentCode := range m.Continents {
						if userContinent == continentCode {
							continue
						}

						for countryCode := range m.Continents[continentCode].Countries {
							if finalLink, err = fileCheck(m.Continents[continentCode].Countries[countryCode].Mirrors[:], vars["filePath"], userCountry); err == nil {
								break
							}
						}

						if finalLink != "" {
							break
						}
					}
				}

				if finalLink == "" {
					// TODO: redirect do other CDN
					log.Warnf("user %s:%s could not find a file %s on any mirror", userContinent, userCountry, vars["filePath"])
					w.WriteHeader(http.StatusNotFound)
					return
				}
			}
		} else {
			for continentCode := range m.Continents {
				for countryCode := range m.Continents[continentCode].Countries {
					if finalLink, err = fileCheck(m.Continents[continentCode].Countries[countryCode].Mirrors[:], vars["filePath"], userCountry); err == nil {
						break
					}
				}

				if finalLink != "" {
					break
				}
			}

			if finalLink == "" {
				// TODO: redirect do other CDN
				log.Warnf("user %s:%s could not find a file %s on any mirror", userContinent, userCountry, vars["filePath"])
				w.WriteHeader(http.StatusNotFound)
				return
			}
		}

		w.Header().Add("Location", finalLink)
		w.WriteHeader(http.StatusFound)
	}
}
