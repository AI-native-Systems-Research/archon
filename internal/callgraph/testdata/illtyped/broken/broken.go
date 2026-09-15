// Package broken does not type-check. It is the cause: every package that
// imports it is reported ill-typed too, though only this one is wrong.
package broken

var Answer int = "not an int"

func Get() int { return Answer }
