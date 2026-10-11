package mirrors

import "testing"

// fixture:
//
//	IT: one down mirror, one stale (old version), one healthy
//	FR: one healthy
//	DE: all mirrors down
//	EMPTY: no mirrors at all
//	EU/DEFAULT, global DEFAULT, MASTER: one healthy each
func testRoot() Root {
	return Root{Continents: map[string]Continent{
		"MASTER": {Countries: map[string]Country{
			"MASTER": {Mirrors: []Mirror{{Name: "master", Url: "http://master", Version: 9999}}},
		}},
		"DEFAULT": {Countries: map[string]Country{
			"DEFAULT": {Mirrors: []Mirror{{Name: "global", Url: "http://global", Version: 100}}},
		}},
		"EU": {Countries: map[string]Country{
			"DEFAULT": {Mirrors: []Mirror{{Name: "eu-default", Url: "http://eu-default", Version: 100}}},
			"IT": {Mirrors: []Mirror{
				{Name: "it-down", Url: "http://it-down", Version: 100, Down: true},
				{Name: "it-stale", Url: "http://it-stale", Version: 10},
				{Name: "it-good", Url: "http://it-good", Version: 100},
			}},
			"FR": {Mirrors: []Mirror{
				{Name: "fr-good", Url: "http://fr-good", Version: 100},
			}},
			"DE": {Mirrors: []Mirror{
				{Name: "de-down", Url: "http://de-down", Version: 100, Down: true},
			}},
			"EMPTY": {Mirrors: []Mirror{}},
		}},
	}}
}

// With one of the three IT mirrors down and one stale, the old
// single-draw lookup fell back to the continent DEFAULT ~2/3 of the
// time; the healthy in-country mirror must now always be picked.
func TestLookupSkipsUnhealthyCountryMirrors(t *testing.T) {
	root := testRoot()
	for i := 0; i < 200; i++ {
		if m := root.Lookup("/pool/x", 50, "EU", "IT"); m.Name != "it-good" {
			t.Fatalf("iteration %d: expected it-good, got %s", i, m.Name)
		}
	}
}

func TestLookupFallsBackThroughLevels(t *testing.T) {
	root := testRoot()
	cases := []struct {
		name      string
		continent string
		country   string
		version   uint64
		want      string
	}{
		{"unknown country uses continent default", "EU", "XX", 50, "eu-default"},
		{"empty country list does not panic", "EU", "EMPTY", 50, "eu-default"},
		{"country with all mirrors down", "EU", "DE", 50, "eu-default"},
		{"unknown continent uses global default", "AP", "JP", 50, "global"},
		{"stale everywhere falls to master", "EU", "FR", 5000, "master"},
		{"freshness boundary is inclusive", "EU", "FR", 100, "fr-good"},
	}
	for _, c := range cases {
		if m := root.Lookup("/pool/x", c.version, c.continent, c.country); m.Name != c.want {
			t.Errorf("%s: got %s, want %s", c.name, m.Name, c.want)
		}
	}
}
