package main

import (
	"flag"
	"os"
)

type config struct {
	Addr        string
	DatabaseURL string
}

func loadConfig() config {
	addr := flag.String("addr", "", "HTTP listen address (default :3001)")
	flag.Parse()

	cfg := config{
		Addr:        ":3001",
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
	if *addr != "" {
		cfg.Addr = *addr
	}
	if v := os.Getenv("ADDR"); v != "" && *addr == "" {
		cfg.Addr = v
	}
	return cfg
}
