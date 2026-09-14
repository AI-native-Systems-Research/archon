// Package app calls store.Store from every position a call can occupy: plainly,
// from a closure, from a go statement, from a defer, and from the method of a
// generic type.
package app

import (
	"fmt"
	"io"

	"example.com/iface/store"
)

func Serve(s store.Store) { s.Get("plain") }

func InClosure(s store.Store) {
	f := func() { s.Get("closure") }
	f()
}

func InGo(s store.Store) { go s.Get("goroutine") }

func InDefer(s store.Store) { defer s.Get("defer") }

type Box[T any] struct{ S store.Store }

func (b Box[T]) Fill() { b.S.Get("box") }

func UseBox(s store.Store) { Box[int]{S: s}.Fill() }

// Two reaches the same method through two different interfaces, so one edge
// carries two candidate witnesses.
func Two(s store.Store, g store.Getter) {
	s.Get("store")
	g.Get("getter")
}

// MethodValue dispatches inside a wrapper go/ssa synthesises, which has no
// declaration to attribute the call to. The static walk does not see it either,
// because the call expression is f(), not s.Get.
func MethodValue(s store.Store) {
	f := s.Get
	f("method value")
}

// Direct calls a method on a concrete type, which the resolver handles through
// go/types selections rather than as a plain identifier.
func Direct(m store.Mem) { m.Get("concrete") }

// CallStoreHelper calls across packages by qualifier.
func CallStoreHelper() int { return store.Helper() }

// WriteTo dispatches through a standard-library interface, so the callees CHA
// offers for this site include every implementation of io.Writer in the program,
// nearly all of them outside this module.
func WriteTo(w io.Writer) { w.Write(nil) }

// Registered is a closure in a package-level variable initializer. It sits under
// the synthetic package initializer, which has no object of its own, so the
// interface call inside it has no declaration to be attributed to either. The
// static walk never looks inside a variable initializer at all.
var Registered = func(s store.Store) { s.Get("registered") }

// OutOfModule calls something this module does not define. Nothing may leave the
// module.
func OutOfModule() string { return fmt.Sprint("leaving") }

// Helper and CallHelper are an ordinary direct call, which the static walk
// already resolves.
func Helper() {}

func CallHelper() { Helper() }
