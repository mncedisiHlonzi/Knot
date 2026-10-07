// Command knot is the entry point for the Knot backend.
//
// KNOT-001 scope: this binary only prints a startup line. There is deliberately
// no HTTP server, no routing, and no data access here — those belong to later,
// separately approved tasks.
package main

import (
	"fmt"

	"github.com/knot/backend/internal/appinfo"
)

func main() {
	fmt.Printf("%s %s starting...\n", appinfo.Name, appinfo.Version)
}
