package handlers

import (
	"errors"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/oschwald/geoip2-golang"
	log "github.com/sirupsen/logrus"
	"net"
	"net/http"
	"net/url"
	"parrot-redirector/types"
)

const CloudflareMirror = "https://mirror.parrot.sh/mirrors/parrot"

func resolveLink(basePath string, postfixPath string) (string, error) {
	base, err := url.Parse(basePath)
	if err != nil {
		return "", fmt.Errorf("error parsing base URL: %v", err)
	}

	postfix, err := url.Parse(base.Path + "/" + postfixPath)
	if err != nil {
		return "", fmt.Errorf("error parsing postfix URL: %v", err)
	}

	fileLink := base.ResolveReference(postfix).String()
	return fileLink, nil
}

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

		fileLink, err := resolveLink(value.Url, filePath)
		if err != nil {
			log.Errorf("error resolving link: %v", err)
			continue
		}

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
func NewFilesHandler(m types.MirrorsYAML, filesList *[]string) func(w http.ResponseWriter, r *http.Request) {
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
		for _, f := range *filesList {
			if f == vars["filePath"] {
				found = true
				break
			}
		}

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
						finalLink, err = resolveLink(CloudflareMirror, vars["filePath"])
						if err != nil {
							log.Error(err)
							w.WriteHeader(http.StatusInternalServerError)
							return
						}
						log.Warnf("user %s:%s could not find a file %s on any mirror, redirected to cloudflare",
							userContinent, userCountry, vars["filePath"])
					}
				}
			} else {
				// fallback to other continents, choosing random country
				for countryCode, _ := range m.Continents[userContinent].Countries {
					if finalLink, err = fileCheck(m.Continents[userContinent].Countries[countryCode].Mirrors[:],
						vars["filePath"], userCountry); err == nil {
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
					finalLink, err = resolveLink(CloudflareMirror, vars["filePath"])
					if err != nil {
						log.Error(err)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					log.Warnf("user %s:%s could not find a file %s on any mirror, redirected to cloudflare",
						userContinent, userCountry, vars["filePath"])
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
				finalLink, err = resolveLink(CloudflareMirror, vars["filePath"])
				if err != nil {
					log.Error(err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				log.Warnf("user %s:%s could not find a file %s on any mirror, redirected to cloudflare",
					userContinent, userCountry, vars["filePath"])
			}
		}

		w.Header().Add("Location", finalLink)
		w.WriteHeader(http.StatusFound)
	}
}
