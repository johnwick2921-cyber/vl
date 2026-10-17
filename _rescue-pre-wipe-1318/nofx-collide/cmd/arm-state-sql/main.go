// arm-state-sql prints the canonical arm predicate for read-only watches and
// audit queries. It opens no database and performs no deployment action.
package main

import (
	"flag"
	"fmt"
	"nofx/store"
)

func main() {
	terminal := flag.Bool("terminal", false, "emit the terminal predicate (default: non-terminal)")
	flag.Parse()
	if *terminal {
		fmt.Println(store.TerminalArmStateSQL())
		return
	}
	fmt.Println(store.NonTerminalArmStateSQL())
}
