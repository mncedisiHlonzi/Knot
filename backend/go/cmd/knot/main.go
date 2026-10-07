// Command knot is the entry point for the Knot backend.
//
// KNOT-002 scope: load configuration, then print a single startup line. There is
// deliberately no HTTP server, no routing, and no data access here — the config
// package stores the Postgres DSN and Redis address but opens neither. Real
// connectivity belongs to later, separately approved tasks.
package main

import (
	"fmt"
	"os"

	"github.com/knot/backend/internal/appinfo"
	"github.com/knot/backend/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: configuration error: %v\n", appinfo.Name, err)
		os.Exit(1)
	}

	// Only the environment name is printed. The Postgres DSN and Redis address
	// may carry credentials and must never be logged.
	fmt.Printf("%s %s starting (env=%s)\n", appinfo.Name, appinfo.Version, cfg.Env)
}
