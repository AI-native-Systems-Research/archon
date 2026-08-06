# Architectural Review: Vault Integration PR (Diff-Only)

## Summary of Architectural Changes

### New Components
- **`internal/envvar/envvar.go`** - Configuration abstraction layer with a `Provider` interface for secrets retrieval
- **`internal/envvar/vault/vault.go`** - Vault-specific implementation of the `Provider` interface
- **`internal/envvar/envvartesting/provider.gen.go`** - Counterfeiter-generated fake for testing

### New External Dependencies
- `github.com/hashicorp/vault/api v1.0.4` - HashiCorp Vault client
- `github.com/joho/godotenv v1.3.0` - Environment file loader
- Transitive dependencies: go-cleanhttp, go-retryablehttp, go-rootcerts, go-sockaddr, hcl, mitchellh/mapstructure, go-jose.v2

### Changed Contracts/Interfaces
- **`Provider` interface** (`internal/envvar/envvar.go:12-14`): New contract requiring `Get(key string) (string, error)`
- **`Configuration.Get`**: Dual-path lookup - checks env var directly, falls back to provider when `<KEY>_SECURE` exists
- **`newDB()`**: Signature changed from `() (*sql.DB, error)` to `(conf *envvar.Configuration) *sql.DB` - no longer returns error, uses log.Fatal internally

---

## Architectural Risks and Gaps

### CRITICAL - Security Concerns

- **Vault credentials in environment variables**: `VAULT_TOKEN` is read from env (`os.Getenv("VAULT_TOKEN")` in main.go). Token lives in plaintext in process environment and `env.example`.
- **`env.example` contains hardcoded token**: `VAULT_TOKEN="myroot"` is committed - risk of copy-paste to production.
- **Vault address defaults to HTTP**: `VAULT_ADDRESS="http://0.0.0.0:8300"` - no TLS by default.

### HIGH - Untested Code Paths

- **`internal/envvar/vault/vault.go` error paths partially tested**: The `invalid data in secret` and `secret value in data is not string` error branches are NOT exercised in the test suite - these require specific Vault data structures that the tests don't set up.
- **`envvar.Load()` untested**: The `Load()` function that wraps `godotenv.Load()` has no direct test coverage - only used incidentally via test fixture loading.
- **No test for Configuration when provider is nil**: `New(provider)` accepts any Provider including nil, but `Get()` would panic if `_SECURE` env var is set with a nil provider.

### MEDIUM - Concurrency/Thread Safety

- **`vault.Provider.results` map is not goroutine-safe**: The cache (`map[string]map[string]string`) is accessed without synchronization. Test comment explicitly notes: "Not calling t.Parallel() because vault.Provider is not goroutine safe."
- **Potential production issue**: If `Configuration.Get()` is called concurrently for different keys sharing the same secret path, race condition on map read/write.

### MEDIUM - Error Handling Design

- **`newDB()` uses `log.Fatal` instead of returning errors**: This makes the database initialization untestable and prevents graceful error handling by callers.
- **Silent override**: `Configuration.Get()` returns the provider value when `_SECURE` exists, even if the base env var also has a value - no warning or precedence documentation.

### LOW - Architectural Observations

- **Caching semantics unclear**: `vault.Provider` caches entire secret paths, but cache invalidation is never performed. Long-running processes will never see secret updates.
- **Key format coupling**: The `path:key` format (e.g., `/database:password`) is parsed via `strings.Split(v, ":")` which would break if path contains colons.
- **XXX comments**: Multiple `// XXX: We will revisit this code in future episodes` markers suggest incomplete design.

---

## Recommendations for Reviewers

1. **Require TLS for Vault connection** or document why HTTP is acceptable
2. **Add mutex to `vault.Provider.results`** or document single-threaded usage requirement
3. **Add tests for untested error branches** in vault.go
4. **Consider returning errors from `newDB()`** instead of fatal-ing
5. **Remove or rotate** the example token before merge
