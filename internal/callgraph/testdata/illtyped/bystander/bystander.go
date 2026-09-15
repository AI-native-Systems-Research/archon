// Package bystander is correct Go that imports a broken package, and is itself a
// dependency of importer. It inherited the problem, so it is only worth
// reporting when it is one of the packages asked for.
package bystander

import "example.com/illtyped/broken"

func Also() int { return broken.Get() }
