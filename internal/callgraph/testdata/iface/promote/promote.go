// Package promote embeds a narrower interface and is used through a wider one.
// go/ssa then emits the dispatch only inside the wrapper it synthesises for the
// promoted method, and that wrapper has no declaration — so an edge cannot be
// drawn from it, and dropping it loses a call through an interface.
package promote

import "example.com/iface/store"

type ReadCloser interface {
	Get(k string) []byte
	Close() string
}

type Wrap struct{ store.Store }

func (Wrap) Close() string { return "closed" }

func Use(s store.Store) string {
	var rc ReadCloser = Wrap{s}
	rc.Get("promoted")
	return rc.Close()
}

// Wider embeds ReadCloser, so Outer's promoted Get reaches store.Store.Get
// through two synthesised wrappers rather than one.
type Wider interface {
	Get(k string) []byte
	Close() string
	Extra() string
}

type Outer struct{ ReadCloser }

func (Outer) Extra() string { return "extra" }

func UseNested(s store.Store) string {
	var w Wider = Outer{Wrap{s}}
	w.Get("nested")
	return w.Extra()
}
