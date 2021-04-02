package handlers

import (
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"net/http"
	"net/url"
	"parrot-redirector/types"
)

// Route which redirects to a closest mirror
func NewFilesHandler(m types.MirrorsYAML, filesList []string) func(w http.ResponseWriter, r *http.Request) {
	return func (w http.ResponseWriter, r *http.Request) {
		// performing a sync health check sorted by speed
		/*
			geoip request should be here to get CIDR->ASN->CITY->COUNTRY->CONTINENT->FALLBACK
			then mirrors should be sorted by speed and accessibility
		*/
		vars := mux.Vars(r)
		// checking if file should be in the repo
		found := false
		for _, f := range filesList {
			if f == vars["filePath"] {
				found = true
				break
			}
		}

		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		finalLink := ""
		for _, value := range m.Mirrors {
			base, err := url.Parse(value.Url)
			if err != nil {
				log.Errorf("error parsing base URL: %v", err)
				continue
			}

			postfix, err := url.Parse(base.Path + "/" + vars["filePath"])
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
				finalLink = fileLink
				break
			}
		}
		if finalLink == "" {
			// this means all repos are missing requested file
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Add("Location", finalLink)
		w.WriteHeader(http.StatusFound)
	}
}


