// Command registry imports plug only for its initializer, the way a cobra or
// database/sql program does.
package main

import (
	"fmt"

	_ "example.com/registry/plug"
	"example.com/registry/reg"
)

func main() { fmt.Println(reg.Dispatch()) }
