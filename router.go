package main

import (
	. "github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/oschwald/geoip2-golang"
	log "github.com/sirupsen/logrus"
	"net/http"
	"parrot-redirector/handlers"
)

func Router(geoDB *geoip2.Reader) http.Handler  {
	r := mux.NewRouter()

	r.HandleFunc("/files/{filePath:.*}", handlers.
		NewFilesHandler(
		mirrorsYAML,
		geoDB)).Methods("GET")

	loggedHandler := LoggingHandler(log.New().Writer(), r)

	return CORS(
		AllowedHeaders([]string{"X-Requested-With", "Content-Type", "Authorization"}),
		AllowedMethods([]string{"GET", "POST", "PUT", "OPTIONS"}))(loggedHandler)
}