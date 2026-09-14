// Package app calls store.Store from every position a call can occupy: plainly,
// from a closure, from a go statement, from a defer, and from the method of a
// generic type.
package app

import "example.com/iface/store"

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

// Helper and CallHelper are an ordinary direct call, which the static walk
// already resolves.
func Helper() {}

func CallHelper() { Helper() }
