// Package apply calls a function value. Nothing in this module ever passes it
// hidden.Squirrel, but that function has the same signature as its parameter.
package apply

func Apply(f func(int) int) int { return f(1) }
