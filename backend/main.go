package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
)

func splitCSV(input string) []string {
	parts := strings.Split(input, ",")
	out := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataPath := flag.String("data", "./data/state.json", "path to state file")
	corsOrigins := flag.String("cors", "http://localhost:5173", "comma-separated allowed CORS origins")
	staticDir := flag.String("static", "", "optional path to built frontend assets")
	flag.Parse()
	store, err := NewStore(*dataPath)
	if err != nil {
		log.Fatalf("load store: %v", err)
	}
	service := NewService(store)
	api := NewAPI(service, splitCSV(*corsOrigins), *staticDir)
	server := &http.Server{Addr: *addr, Handler: api.Handler()}
	log.Printf("interstellar trade backend listening on %s", *addr)
	log.Fatal(server.ListenAndServe())
}
