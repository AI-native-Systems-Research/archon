// Package store holds the interface and its one implementation. It also holds
// two package initializers, whose types.Func.FullName is the same string.
package store

type Store interface{ Get(k string) []byte }

// Getter is satisfied by the same method, so a call site through it produces the
// same edge as one through Store, with a different witness.
type Getter interface{ Get(k string) []byte }

type Mem struct{}

func (m Mem) Get(k string) []byte { return []byte(k) }

// Helper is called through a package qualifier, which the resolver handles on a
// different branch from a plain identifier.
func Helper() int { return loaded }

var loaded int

func init() { loaded++ }

func init() { loaded += 2 }
