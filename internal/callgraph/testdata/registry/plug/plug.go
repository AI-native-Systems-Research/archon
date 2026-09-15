// Package plug registers its handler from a package initializer, which is the
// only place P is ever instantiated.
package plug

import "example.com/registry/reg"

type P struct{}

func (P) Handle() string { return "p" }

func init() { reg.Register(P{}) }
