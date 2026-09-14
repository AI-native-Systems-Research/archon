// Package importer is correct Go. It is ill-typed only because broken is.
package importer

import "example.com/illtyped/broken"

func Use() int { return broken.Get() }
