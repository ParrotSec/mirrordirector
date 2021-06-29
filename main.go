package main

import (
	"context"
	"flag"
	"github.com/oschwald/geoip2-golang"
	"net/http"
	"os"
	"os/signal"
	"parrot-redirector/db"
	"parrot-redirector/types"
	"parrot-redirector/watcher"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

var mirrorsYAML types.MirrorsYAML

var config struct {
	gracefulExitWait time.Duration
	debug            bool
	geoDBPath        string
	cachePath        string
	repoPath         string
	mirrorsPath      string
}

func init() {
	flag.DurationVar(&config.gracefulExitWait, "graceful-timeout", time.Second*15,
		"the duration for which the server gracefully wait for existing connections to finish - e.g. 15s or 1m")
	path, err := filepath.Abs("repository")
	if err != nil {
		log.Fatal(err)
	}
	flag.StringVar(&config.repoPath, "r", path,
		"path to a repository, set to 'repository' as a default")
	flag.StringVar(&config.geoDBPath, "g", "country.mmdb",
		"path to country geo database, set to 'country.mmdb' as a default")
	flag.StringVar(&config.cachePath, "c", "cache.db",
		"path to sqlite3 database which is used caching")
	flag.StringVar(&config.mirrorsPath, "m", "mirrors.yaml",
		"path to mirrors.yaml file which has routing rules with formatting of" +
		" continents:[CONTINENT]:[COUNTRY]:mirrors:[mirrors urls], CONTINENT and COUNTRY ISO 3166-1")
	flag.BoolVar(&config.debug, "d", false, "debug mode")
	flag.Parse()

	if config.debug {
		if err := os.Setenv("GO_ENV", "DEBUG"); err != nil {
			log.Fatal(err)
		}
	}

	mirrorsData, err := os.ReadFile(config.mirrorsPath)
	if err != nil {
		log.Fatalf("mirrors.yaml file missing")
	}
	err = yaml.Unmarshal(mirrorsData, &mirrorsYAML)
	if err != nil {
		log.Fatalf("parsing mirrors.yaml error: %v", err)
	}
	if _, err := os.Stat(config.repoPath); os.IsNotExist(err) {
		if err := os.MkdirAll(config.repoPath, 0775); err != nil {
			log.Fatal(err)
		}
	}

	// Check if sqlite3 database file exists
	if _, err := os.Stat(config.cachePath); os.IsNotExist(err) {
		if _, err := os.Create(config.cachePath); err != nil {
			log.Fatal(err)
		}
	}
	db.CreateCacheDatabase(config.cachePath)
	for continentCode, continent := range mirrorsYAML.Continents {
		for countryCode, country := range continent.Countries {
			for _, m := range country.Mirrors {
				db.Cache.AddMirror(m.Name, m.Url, continentCode, countryCode,
					m.BlockedCountries)
			}
		}
	}
}

func main() {
	var prod bool
	prodStr, exists := os.LookupEnv("PROD")
	if !exists {
		prod = false
	} else {
		if prodStr == "1" {
			prod = true
		}
	}

	var serverAddr string
	if prod {
		serverAddr = ":80"
	} else {
		serverAddr = ":8000"
	}

	geoDB, err := geoip2.Open(config.geoDBPath)
	if err != nil {
		log.Fatal("Error opening geolite db")
	}

	srv := &http.Server{
		Addr: serverAddr,
		// Good practice to set timeouts to avoid Slowloris attacks.
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      Router(geoDB), // Pass our instance of gorilla/mux in.
	}
	go watcher.InitWatcher(config.repoPath)
	// Run our server in a goroutine so that it doesn't block.
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	log.WithFields(log.Fields{
		"address": srv.Addr,
	}).Info("Server successfully started")

	c := make(chan os.Signal, 1)
	// We'll accept graceful shutdowns when quit via SIGINT (Ctrl+C)
	// SIGKILL, SIGQUIT or SIGTERM (Ctrl+/) will not be caught.
	signal.Notify(c, os.Interrupt)

	// Block until we receive our signal.
	<-c

	// Create a deadline to wait for.
	ctx, cancel := context.WithTimeout(context.Background(), config.gracefulExitWait)
	defer cancel()
	// Doesn't block if no connections, but will otherwise wait
	// until the timeout deadline.
	srv.Shutdown(ctx)
	// Optionally, you could run srv.Shutdown in a goroutine and block on
	// <-ctx.Done() if your application should wait for other services
	// to finalize based on context cancellation.
	log.Println("Shutting down")
	os.Exit(0)
}
