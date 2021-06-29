package handlers

import (
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/mux"
	"github.com/oschwald/geoip2-golang"
	log "github.com/sirupsen/logrus"
	"net"
	"net/http"
	"net/url"
	"os"
	"parrot-redirector/db"
	"parrot-redirector/types"
)

type WatcherMessage struct{
	filePath string
	action fsnotify.Op
}

type Point struct {
	count int
	link chan string
}

// SyncPoint marks if a goroutine is observing selected fine in selected country
var SyncPoint = make(map[string]map[string] bool)

// In case a file was added while request was pending we need to modify SyncPoint which
// is running in a separate goroutine
var WatcherChan = make(chan WatcherMessage)

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

	// check cached records
	if link, _, err := db.Cache.GetFileLink(filePath, userCountryCode); err == nil {
		return link, nil
	}

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
			continue
		}

		if resp.StatusCode == http.StatusOK {
			// add a record for that specific file
			if err := db.Cache.AddRecord(filePath, value.Name, false); err != nil {
				return "", err
			}
			return fileLink, nil
		}
	}
	return "", db.ErrFileWasNotFound
}


// Route which redirects to a closest mirror
func NewFilesHandler(m types.MirrorsYAML, geoDB *geoip2.Reader) func(w http.
	ResponseWriter,
	r *http.Request) {
	for _, continent := range m.Continents {
		for countryCode := range continent.Countries {
			SyncPoint[countryCode] = make(map[string]bool)
			for _, f := range db.Cache.GetAllFiles() {
				SyncPoint[countryCode][f] = false
			}
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// performing a sync health check sorted by speed

		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			log.Errorf("Error splitting ip and port")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		dbResult, err := geoDB.Country(net.ParseIP(host))
		if err != nil {
			log.Errorf("Error getting country from geodb")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		userCountry := dbResult.Country.IsoCode
		userContinent := dbResult.Continent.Code

		if os.Getenv("GO_ENV") == "DEBUG" {
			userCountry = "UA"
			userContinent = "EU"
		}

		vars := mux.Vars(r)

		if !db.Cache.ExistsFile(vars["filePath"]) {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var finalLink string

		// If record does not exist - send fallback link and start a check request
		if link, mirrorName, err := db.Cache.GetFileLink(vars["filePath"],
			userCountry); err == nil {
			// Check if link still has the file
			resp, err := http.Head(link)
			if err != nil {
				log.Errorf("error initiating HEAD request: %v", err)
			} else {
				if resp.StatusCode == http.StatusOK {
					finalLink = link
				} else {
					if err := db.Cache.DeleteRecord(mirrorName,
						vars["filePath"]); err != nil {
						log.Error(err)
					}
				}
				finalLink = link
			}
		} else {
			finalLink, err = resolveLink(CloudflareMirror, vars["filePath"])
			if err != nil {
				log.Error(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			log.Warnf("user %s:%s could not find a file %s on any mirror, redirected to cloudflare",
				userContinent, userCountry, vars["filePath"])

			// We need to check if the same file by a request from the same continent
			// and country was already fulfilled
			if SyncPoint[userCountry][vars["filePath"]] {
				return
			}

			SyncPoint[userCountry][vars["filePath"]] = true
			go (func() {
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
								if err := db.Cache.AddRecord(vars["filePath"], "",
									true); err != nil {
									log.Warn(err)
								}
								finalLink, err = resolveLink(CloudflareMirror, vars["filePath"])
								if err != nil {
									log.Error(err)
									w.WriteHeader(http.StatusInternalServerError)
									return
								}
								log.Warnf("user %s:%s could not find a file %s on any mirror, redirected to cloudflare",
									userContinent, userCountry, vars["filePath"])
							}
						} else {

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
							if err := db.Cache.AddRecord(vars["filePath"], "",
								true); err != nil {
								log.Warn(err)
							}
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
						if err := db.Cache.AddRecord(vars["filePath"], "",
							true); err != nil {
							log.Warn(err)
						}
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

			})()
		}



		w.Header().Add("Location", finalLink)
		w.WriteHeader(http.StatusFound)
	}
}
