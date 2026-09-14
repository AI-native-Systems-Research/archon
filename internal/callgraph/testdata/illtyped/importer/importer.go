// Package importer is correct Go. It is ill-typed only because broken is.
package importer

import (
	"example.com/illtyped/broken"
	"example.com/illtyped/broken2"
	"example.com/illtyped/bystander"
)

func Use() int { return broken.Get() + broken2.Get2() + bystander.Also() }
