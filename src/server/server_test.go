package server

import (
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/parrotsec/mirrordirector/files"
	"github.com/parrotsec/mirrordirector/mirrors"
)

// GetLocation must never panic, even without a geoip database or a
// parseable client ip: in those cases it must return DEFAULT so that
// the mirror lookup falls back to the DEFAULT mirrors.
func TestGetLocationNilSafety(t *testing.T) {
	if continent, country := GetLocation(nil, net.ParseIP("1.2.3.4")); continent != "DEFAULT" || country != "DEFAULT" {
		t.Fatalf("expected DEFAULT location with nil mmdb, got %q %q", continent, country)
	}
	if continent, country := GetLocation(nil, nil); continent != "DEFAULT" || country != "DEFAULT" {
		t.Fatalf("expected DEFAULT location with nil mmdb and nil ip, got %q %q", continent, country)
	}
}

func TestGetIP(t *testing.T) {
	newReq := func(xff, xrip, remoteAddr string) *http.Request {
		r := httptest.NewRequest("GET", "https://director.parrot.sh/pool/x", nil)
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		if xrip != "" {
			r.Header.Set("X-Real-Ip", xrip)
		}
		if remoteAddr != "" {
			r.RemoteAddr = remoteAddr
		}
		return r
	}
	// httptest defaults RemoteAddr to 192.0.2.1:1234 (TEST-NET-1,
	// routable for our purposes)
	cases := []struct {
		name, xff, xrip, remoteAddr, want string
	}{
		{"single xff", "203.0.113.7", "", "", "203.0.113.7"},
		{"list picks leftmost client", "203.0.113.7, 10.0.0.1", "", "", "203.0.113.7"},
		{"list with spaces", "  203.0.113.7 ,  192.0.2.1 ", "", "", "203.0.113.7"},
		{"skips unknown entry", "unknown, 203.0.113.7", "", "", "203.0.113.7"},
		{"skips garbage entry", "not-an-ip, 203.0.113.7", "", "", "203.0.113.7"},
		{"skips private entry", "10.0.0.5, 203.0.113.7", "", "", "203.0.113.7"},
		{"skips loopback entry", "127.0.0.1, 203.0.113.7", "", "", "203.0.113.7"},
		{"all-private list falls back to remoteaddr", "10.0.0.5, 172.16.0.1", "", "", "192.0.2.1"},
		{"x-real-ip used when no xff", "", "203.0.113.8", "", "203.0.113.8"},
		{"x-real-ip list", "", "10.0.0.1, 203.0.113.8", "", "203.0.113.8"},
		{"xff wins over x-real-ip", "203.0.113.7", "203.0.113.8", "", "203.0.113.7"},
		{"empty xff uses remoteaddr", "", "", "", "192.0.2.1"},
		{"remoteaddr ipv4", "", "", "203.0.113.9:443", "203.0.113.9"},
		{"remoteaddr ipv6", "", "", "[2001:db8::1]:443", "2001:db8::1"},
	}
	for _, c := range cases {
		if got := GetIP(newReq(c.xff, c.xrip, c.remoteAddr)); got != c.want {
			t.Errorf("%s: GetIP() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAptEscapePath(t *testing.T) {
	cases := map[string]string{
		"/pool/f/fake/fake_1.0+dfsg-1_amd64.deb":  "/pool/f/fake/fake_1.0%2bdfsg-1_amd64.deb",
		"/pool/f/fake/fake_2.0~rc1-1_amd64.deb":   "/pool/f/fake/fake_2.0%7erc1-1_amd64.deb",
		"/pool/f/fake/fake_3.0~bpo13+1_amd64.deb": "/pool/f/fake/fake_3.0%7ebpo13%2b1_amd64.deb",
		"/pool/f/fake/a b.deb":                    "/pool/f/fake/a%20b.deb",
		"/pool/f/fake/a%b.deb":                    "/pool/f/fake/a%25b.deb",
		"/pool/f/fake/plain_1.2.3-4_amd64.deb":    "/pool/f/fake/plain_1.2.3-4_amd64.deb",
		"/pool/f/fake/über.deb":                   "/pool/f/fake/%c3%bcber.deb",
	}
	for in, want := range cases {
		if got := aptEscapePath(in); got != want {
			t.Errorf("aptEscapePath(%q) = %q, want %q", in, got, want)
		}
	}
}

// The redirect Location must always carry the request path in apt's
// canonical encoding (lowercase %xx for '%', '+', '~', ' ', control
// and non-ASCII bytes), no matter how the request itself was
// encoded on the wire - CDNs between apt and the director may
// normalize percent-encoding (e.g. decoding %7e back to '~').
func TestRedirectTargetEncoding(t *testing.T) {
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	root := mirrors.Root{Continents: map[string]mirrors.Continent{
		"MASTER": {Countries: map[string]mirrors.Country{
			"MASTER": {Mirrors: []mirrors.Mirror{{Name: "master", Url: "http://master", Version: 9999}}},
		}},
		"DEFAULT": {Countries: map[string]mirrors.Country{
			"DEFAULT": {Mirrors: []mirrors.Mirror{{Name: "m", Url: "http://m", Version: 9999}}},
		}},
	}}
	fset := files.Fileset{Files: map[string]files.File{
		"/pool/f/fake/fake_1.0+dfsg-1_amd64.deb": {Uri: "/pool/f/fake/fake_1.0+dfsg-1_amd64.deb", Version: 1},
		"/pool/f/fake/fake_2.0~rc1-1_amd64.deb":  {Uri: "/pool/f/fake/fake_2.0~rc1-1_amd64.deb", Version: 1},
	}}
	state := &atomic.Value{}
	state.Store(State{Root: root, Fileset: fset})
	h := Handler(nil, state)

	cases := []struct {
		name, target, wantLocation string
	}{
		// the form apt uses on the wire
		{"plus pct", "http://x/pool/f/fake/fake_1.0%2bdfsg-1_amd64.deb", "http://m/pool/f/fake/fake_1.0%2bdfsg-1_amd64.deb"},
		// the form a request takes after a CDN decoded it
		{"plus literal", "http://x/pool/f/fake/fake_1.0+dfsg-1_amd64.deb", "http://m/pool/f/fake/fake_1.0%2bdfsg-1_amd64.deb"},
		{"tilde pct", "http://x/pool/f/fake/fake_2.0%7erc1-1_amd64.deb", "http://m/pool/f/fake/fake_2.0%7erc1-1_amd64.deb"},
		{"tilde literal", "http://x/pool/f/fake/fake_2.0~rc1-1_amd64.deb", "http://m/pool/f/fake/fake_2.0%7erc1-1_amd64.deb"},
		// unknown file: master fallback must be encoded too
		{"fallback tilde", "http://x/pool/f/fake/other_1.0~bpo-1_amd64.deb", "http://master/pool/f/fake/other_1.0%7ebpo-1_amd64.deb"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", c.target, nil))
		if w.Code != http.StatusTemporaryRedirect {
			t.Errorf("%s: status = %d, want 307", c.name, w.Code)
		}
		if loc := w.Header().Get("Location"); loc != c.wantLocation {
			t.Errorf("%s: Location = %q, want %q", c.name, loc, c.wantLocation)
		}
	}
}

// The scanner goroutine publishes fresh State snapshots while
// handlers serve requests. This simulates that pattern so the race
// detector (go test -race) can verify that published snapshots are
// never mutated in place (copy-on-write discipline).
func TestStateCopyOnWriteRace(t *testing.T) {
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	baseRoot := mirrors.Root{Continents: map[string]mirrors.Continent{
		"MASTER": {Countries: map[string]mirrors.Country{
			"MASTER": {Mirrors: []mirrors.Mirror{{Name: "master", Url: "http://master", Version: 9999}}},
		}},
		"DEFAULT": {Countries: map[string]mirrors.Country{
			"DEFAULT": {Mirrors: []mirrors.Mirror{{Name: "m", Url: "http://m", Version: 9999}}},
		}},
	}}
	baseFiles := files.Fileset{Files: map[string]files.File{
		"/pool/a": {Uri: "/pool/a", Version: 1},
	}}

	state := &atomic.Value{}
	state.Store(State{Root: baseRoot, Fileset: baseFiles})

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// scanner: load last snapshot, deep-copy it, mutate the copy,
	// publish it - like the rescan loop in main() does
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			s := state.Load().(State)
			newRoot := s.Root.DeepCopy()
			newFiles := s.Fileset.DeepCopy()
			newRoot.Continents["DEFAULT"].Countries["DEFAULT"].Mirrors[0].Version = uint64(i)
			newFiles.Files["/pool/a"] = files.File{Uri: "/pool/a", Version: uint64(i)}
			state.Store(State{Root: newRoot, Fileset: newFiles})
		}
		close(stop)
	}()

	// readers: serve requests against the published snapshots
	h := Handler(nil, state)
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://x/pool/a", nil))
				}
			}
		}()
	}
	wg.Wait()
}
