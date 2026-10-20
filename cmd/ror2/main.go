// Command ror2 is a terminal mod manager for Risk of Rain 2 on Mac (Sikarugir/Wine).
package main

import (
	"os"

	"ror2mm/internal/cli"
	"ror2mm/internal/tui"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], tui.Run))
}
