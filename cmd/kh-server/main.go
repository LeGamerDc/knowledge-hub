package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/legamerdc/knowledge-hub/internal/server/handlers"
	"github.com/legamerdc/knowledge-hub/internal/server/service"
)

func main() {
	addr := flag.String("addr", ":18080", "listen address")
	flag.Parse()

	svc := &service.StubService{}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	handlers.HandlerFromMux(svc, r)

	fmt.Printf("Knowledge Hub API Server listening on %s\n", *addr)
	if err := http.ListenAndServe(*addr, r); err != nil {
		log.Fatal(err)
	}
}
