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
		// The redirect target must byte-match the URI apt queued,
		// or apt won't update the mirror name it displays: apt
		// percent-encodes '%', '+', '~', ' ', control and non-ASCII
		// bytes as lowercase %xx (Acquire::URIEncode defaults to
		// "+~ "). We rebuild that canonical encoding from the decoded
		// path instead of trusting the request's raw encoding,
		// because proxies in between (the CDN in front of the
		// director) may normalize percent-encoding before forwarding:
		// CDNs commonly decode %7e back to '~', an RFC 3986
		// unreserved character.
		requestPath := aptEscapePath(r.URL.Path)
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

// aptEscapePath re-encodes a decoded URL path the way apt encodes
// the URIs it queues (see Acquire::URIEncode, default "+~ "): '%',
// '+', '~', ' ', control and non-ASCII bytes become lowercase %xx,
// everything else is left untouched. The result byte-matches apt's
// internal URI no matter how the request was encoded on the wire.
func aptEscapePath(p string) string {
	const hex = "0123456789abcdef"
	var b strings.Builder
	b.Grow(len(p) + 8)
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '%' || c == '+' || c == '~' || c == ' ' || c < 0x20 || c >= 0x7f {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
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
