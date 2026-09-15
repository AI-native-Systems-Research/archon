// Package broken2 is a second cause, so the report has to sort them.
package broken2

var Other int = "also not an int"

func Get2() int { return Other }
