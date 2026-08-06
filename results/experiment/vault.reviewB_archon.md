# Architectural Review: Vault Integration PR

## Summary

This PR introduces HashiCorp Vault integration for secrets management, replacing the previous `DATABASE_URL` environment variable approach with a layered configuration system.

---

## Architectural Changes

### New Components

- **`internal/envvar`** - Core configuration abstraction with `Provider` interface and `Configuration` wrapper
- **`internal/envvar/vault`** - Vault-specific `Provider` implementation using KV v2 secrets engine
- **`internal/envvar/envvartesting`** - Generated counterfeiter fake for testing

### New External Dependencies

- `github.com/hashicorp/vault/api v1.0.4` - Vault client SDK
- `github.com/joho/godotenv v1.3.0` - Dotenv file loading

### New Service Dependency

- **Vault service** (`service:Vault`) - External network service now required at runtime

### Configuration Boundary Changes

| Removed | Added |
|---------|-------|
| `env:DATABASE_URL` | `env:VAULT_TOKEN`, `env:VAULT_ADDRESS`, `env:VAULT_PATH` |
| | `flag:env` (CLI flag for env file) |
| | `DATABASE_HOST`, `DATABASE_PORT`, `DATABASE_USERNAME`, `DATABASE_PASSWORD`, `DATABASE_NAME`, `DATABASE_SSLMODE` |

### New Capabilities

- `cap:net` - Network access for Vault API calls

---

## Architectural Risks and Gaps

### 1. **No Contract Tests for `Provider` Interface** (CRITICAL)

ARCHON flags that three types implement `envvar.Provider` but **no contract test guards this interface**:
- `envvar.Configuration`
- `envvartesting.FakeProvider`
- `vault.Provider`

**Risk**: Implementers may drift in behavior; the fake may not accurately model real provider semantics.

**Recommendation**: Add contract tests that verify all implementations satisfy the same behavioral invariants.

### 2. **Vault Token in Environment Variable** (SECURITY)

`VAULT_TOKEN` is read from the environment (line 100 in `main.go`). The example file `env.example` shows `VAULT_TOKEN="myroot"`.

**Risk**: Tokens may be committed, logged, or leaked through process inspection.

**Recommendation**: Consider AppRole or other renewable authentication methods rather than static tokens in production.

### 3. **No TLS Verification by Default**

`env.example` uses `http://0.0.0.0:8300` (plain HTTP).

**Risk**: Token and secrets transmitted in cleartext in non-dev environments.

**Recommendation**: Enforce HTTPS for production; document dev vs prod configuration.

### 4. **Provider Caching Is Not Thread-Safe**

`vault.Provider` caches results in a plain `map[string]map[string]string` without synchronization (lines 667-668, 743). The test file explicitly notes: "Not calling t.Parallel() because vault.Provider is not goroutine safe."

**Risk**: If `Configuration.Get` is called concurrently (e.g., from multiple goroutines during startup), data races can occur.

**Recommendation**: Add a mutex or use `sync.Map` if concurrent access is expected.

### 5. **Error Handling Shifts to Fatal Exits**

The old `newDB()` returned errors; the new version calls `log.Fatalln` on any configuration failure.

**Risk**: Less composable for testing; harder to test error paths in the caller.

**Recommendation**: Consider returning errors and handling them at main's top level.

### 6. **No Integration Test for Full Configuration Flow**

Unit tests exist for:
- `envvar.Configuration.Get` (with mocked provider)
- `vault.Provider.Get` (with dockerized Vault)

**Gap**: No test verifies the full wiring (`main.go` -> `envvar.Configuration` -> `vault.Provider` -> live Vault).

**Recommendation**: Add an integration test that exercises the complete chain.

### 7. **Vault API Version Pinned at 1.0.4**

The `hashicorp/vault/api` dependency is at v1.0.4 (released 2019). Current versions are 1.x with breaking changes in some areas.

**Risk**: Security patches and API changes may not be available.

**Recommendation**: Evaluate upgrading to a more recent version.

---

## What Is Tested

| Component | Test Coverage |
|-----------|---------------|
| `envvar.Configuration.Get` | Unit tests with fake provider |
| `vault.Provider.Get` | Integration tests with dockerized Vault |
| `envvar.Load` | Used in test fixtures |

---

## Verdict

**Acceptable with reservations.** The architecture is sound (provider abstraction, testable interfaces), but the following should be addressed before merge:

1. Add contract tests for `envvar.Provider` interface
2. Document thread-safety constraints or add synchronization to `vault.Provider`
3. Review security of static token approach for production use
