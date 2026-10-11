package server

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/parrotsec/mirrordirector/files"
	"github.com/parrotsec/mirrordirector/mirrors"

	"github.com/oschwald/geoip2-golang"
)

// State is an immutable snapshot of the mirror tree and the known
// fileset. The scanner goroutine publishes a fresh State after every
// scan cycle by atomically storing it (copy-on-write): snapshots are
// never mutated once published, so handlers can read them without
// any locking.
type State struct {
	Root    mirrors.Root
	Fileset files.Fileset
}

func Serve(url string, state *atomic.Value) {
	mmdb, err := geoip2.Open("/var/lib/GeoIP/GeoLite2-City.mmdb")
	if err != nil {
		log.Panic(err)
	}
	defer mmdb.Close()

	http.HandleFunc("/", Handler(mmdb, state))

	err = http.ListenAndServe(url, nil)
	if err != nil {
		log.Fatalf("Unable to open web server %s: %v\n", url, err)
	}
}

func Handler(mmdb *geoip2.Reader, state *atomic.Value) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// load the current immutable snapshot: the scanner never
		// mutates published snapshots (copy-on-write), so this is
		// safe to read without locks
		s := state.Load().(State)
		Fileset, Root := s.Fileset, s.Root
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
		file, err := Fileset.Lookup(r.URL.Path)
		if err != nil {
			//w.WriteHeader(http.StatusNotFound)
			http.Redirect(w, r, Root.Continents["MASTER"].Countries["MASTER"].Mirrors[rand.Intn(
				len(Root.Continents["MASTER"].Countries["MASTER"].Mirrors),
			)].Url+requestPath, http.StatusTemporaryRedirect)
			return
		}
		ip := net.ParseIP(GetIP(r))
		continent, country := GetLocation(mmdb, ip)
		target := Root.Lookup(file.Uri, file.Version, continent, country)
		log.Printf("|%s|%s|%s|%s|%s|%s|%d\n", ip.String(), continent, country, target.Name, target.Url, file.Uri, file.Version)
		http.Redirect(w, r, target.Url+requestPath, http.StatusTemporaryRedirect)
	}
}

func GetIP(r *http.Request) string {
	// X-Forwarded-For (or X-Real-Ip), when present, can be a
	// comma-separated list: "client, proxy1, proxy2". The client is
	// the leftmost entry, but entries can be missing ("unknown"),
	// unparsable, or private addresses (clients behind corporate
	// proxies), so walk the list and pick the first parsable,
	// globally routable ip to geolocate.
	// note: Header.Get is case-insensitive, it already matches all
	// the X-Forwarded-For / X-Real-Ip casing variants
	for _, header := range []string{"X-Forwarded-For", "X-Real-Ip"} {
		if forwarded := r.Header.Get(header); forwarded != "" {
			for _, hop := range strings.Split(forwarded, ",") {
				ip := net.ParseIP(strings.TrimSpace(hop))
				if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
					continue
				}
				return ip.String()
			}
		}
	}
	// RemoteAddr is ip:port ([v6]:port for ipv6): extract the ip in
	// an ipv6-safe way
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func GetLocation(mmdb *geoip2.Reader, ip net.IP) (string, string) {
	// without a geoip database (or a parseable source ip) we cannot
	// geolocate the client: fall back to the DEFAULT mirrors instead
	// of panicking on a nil lookup
	if mmdb == nil || ip == nil {
		return "DEFAULT", "DEFAULT"
	}
	mmrecord, err := mmdb.City(ip)
	if err != nil {
		log.Print(err)
		return "DEFAULT", "DEFAULT"
	}
	return mmrecord.Continent.Code, mmrecord.Country.IsoCode
}
