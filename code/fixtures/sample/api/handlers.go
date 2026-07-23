package api

import (
	"example.com/sample/auth"
	"example.com/sample/db"
)

// CreateUser handles user creation.
func CreateUser(tok string) *db.User {
	if auth.VerifyToken(tok) {
		return &db.User{ID: 1}
	}
	return nil
}
