package auth

// VerifyToken reports whether a token is valid.
func VerifyToken(tok string) bool { return tok != "" }

// HashToken is a newly exported surface entity.
func HashToken(tok string) string { return tok + "#" }
