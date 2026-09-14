// Package store holds the interface and its one implementation. It also holds
// two package initializers, whose types.Func.FullName is the same string.
package store

type Store interface{ Get(k string) []byte }

type Mem struct{}

func (m Mem) Get(k string) []byte { return []byte(k) }

var loaded int

func init() { loaded++ }

func init() { loaded += 2 }
