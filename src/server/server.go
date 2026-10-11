package server

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strings"

	"github.com/parrotsec/mirrordirector/files"
	"github.com/parrotsec/mirrordirector/mirrors"

	"github.com/oschwald/geoip2-golang"
)

func Serve(url string, Fileset *files.Fileset, Root *mirrors.Root) {
	mmdb, err := geoip2.Open("/var/lib/GeoIP/GeoLite2-City.mmdb")
	if err != nil {
		log.Panic(err)
	}
	defer mmdb.Close()

	http.HandleFunc("/", Handler(mmdb, Fileset, Root))

	err = http.ListenAndServe(url, nil)
	if err != nil {
		log.Fatalf("Unable to open web server %s: %v\n", url, err)
	}
}

func Handler(mmdb *geoip2.Reader, Fileset *files.Fileset, Root *mirrors.Root) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "400 bad request")
			return
		}
		// The redirect target must keep the request path in its original
		// escaped form (r.URL.EscapedPath), not the decoded r.URL.Path:
		// apt percent-encodes characters like '+' in the URIs it queues
		// (e.g. foo_1.0+dfsg-1 becomes foo_1.0%2bdfsg-1), and it only
		// updates the mirror name it displays when the redirect Location
		// ends with the exact same path tail as the queued URI.
		requestPath := r.URL.EscapedPath()
		file, err := Fileset.Lookup(strings.TrimSpace(r.URL.Path))
		if err != nil {
			//w.WriteHeader(http.StatusNotFound)
			http.Redirect(w, r, Root.Continents["MASTER"].Countries["MASTER"].Mirrors[rand.Intn(
				len(Root.Continents["MASTER"].Countries["MASTER"].Mirrors),
			)].Url+requestPath, http.StatusTemporaryRedirect)
			fmt.Fprintf(w, "Directing to Master - Cannot find match")
			return
		}
		ip := net.ParseIP(GetIP(r))
		continent, country := GetLocation(mmdb, ip)
		target := Root.Lookup(file.Uri, file.Version, continent, country)
		log.Printf("|%s|%s|%s|%s|%s|%s|%d\n", ip.String(), continent, country, target.Name, target.Url, file.Uri, file.Version)
		http.Redirect(w, r, target.Url+requestPath, http.StatusTemporaryRedirect)
		fmt.Fprintf(w, "file: %v\nsource: %s\nlocation: %s %s\n", file, ip, continent, country)
	}
}

func GetIP(r *http.Request) string {
	forwarded := r.Header.Get("X-FORWARDED-FOR")
	if forwarded != "" {
		return forwarded
	}
	forwarded = r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		return forwarded
	}
	forwarded = r.Header.Get("X-REAL-IP")
	if forwarded != "" {
		return forwarded
	}
	forwarded = r.Header.Get("X-Real-Ip")
	if forwarded != "" {
		return forwarded
	}
	return strings.Split(r.RemoteAddr, ":")[0]
}

func GetLocation(mmdb *geoip2.Reader, ip net.IP) (string, string) {
	if mmdb == nil {
		return "DEFAULT", "DEFAULT"
	}
	mmrecord, err := mmdb.City(ip)
	if err != nil {
		log.Print(err)
	}
	return mmrecord.Continent.Code, mmrecord.Country.IsoCode
}
