// Package reg is the registry pattern: implementations arrive through Register
// and are called back through the interface, so nothing in the call chain from
// main names them.
package reg

type Handler interface{ Handle() string }

var handlers []Handler

func Register(h Handler) { handlers = append(handlers, h) }

func Dispatch() string {
	out := ""
	for _, h := range handlers {
		out += h.Handle()
	}
	return out
}
