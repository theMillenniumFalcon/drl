package main

import (
	"flag"
	"log"

	"github.com/themillenniumfalcon/drl/store"
)

func main() {
	redisAddr := flag.String("redis-addr", "localhost:6379", "Redis address")
	flag.Parse()

	store, err := store.NewStore(store.Options{
		Addresses: []string{*redisAddr},
	})
	if err != nil {
		log.Fatalf("Failed to create Redis store: %v", err)
	}
}
