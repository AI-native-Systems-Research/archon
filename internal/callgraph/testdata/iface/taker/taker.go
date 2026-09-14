// Package taker separates taking a method value from calling it. The functions
// that call the value are not the ones that take it, and under CHA the wrapper's
// callers are only a signature match — so attributing the site to them names
// functions that contain no method value at all.
package taker

import "example.com/iface/store"

// Take is where the method value is created, and so the only function a reader
// has to go and look at.
func Take(s store.Store) func(string) []byte { return s.Get }

// Invoke calls the value Take made, without taking one itself.
func Invoke(s store.Store) { f := Take(s); f("invoked") }

// Unrelated calls a function value of the same signature and takes no method
// value anywhere.
func Unrelated(f func(string) []byte) { f("unrelated") }

// Held is a method value taken in a package-level variable initializer. The
// function taking it is the synthetic package initializer, which has no
// declaration of its own, so the package is the most that can be named.
var held store.Store = store.Mem{}

var Held = held.Get

// Expr takes a method expression rather than a method value. go/ssa synthesises
// a thunk for it, and a thunk binds no receiver, so it is referenced as a plain
// function value rather than through a closure.
func Expr() func(store.Store, string) []byte { return store.Store.Get }
