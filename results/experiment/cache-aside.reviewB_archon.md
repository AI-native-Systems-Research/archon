# Architectural Review: Cache-Aside Pattern (Memcached)

## Summary

This PR introduces a **cache-aside (read-through)** layer using Memcached to front the Elasticsearch search index. The cache is injected between the service layer and the existing Elasticsearch backend.

---

## Architectural Changes

### New Components
- **`internal/memcached`** package: decorator implementing cache-aside for `Search` operations
- **`cmd/internal/memcached.go`**: factory function for Memcached client instantiation
- **`service:Memcached`**: new external service dependency (Memcached 1.6.9)

### New Dependencies
- **`github.com/bradfitz/gomemcache`**: third-party Memcached client library
- **Infrastructure**: Memcached container added to docker-compose

### Changed Contracts/Interfaces
- **`memcached.Datastore`** interface introduced (Search, Index, Delete)
- **`service.TaskSearchRepository`** now satisfied by `memcached.Task` instead of `elasticsearch.Task` directly
- The wiring in `newServer` swaps `search` (elasticsearch) for `mclient` (memcached-wrapping-elasticsearch)

---

## Architectural Risks and Gaps

### Critical: No Cache Invalidation on Writes
- `Index()` and `Delete()` pass through directly to the underlying Elasticsearch store **without invalidating the cache**
- After a write/delete, stale cached results will be served for up to 25 seconds
- **Consistency risk**: readers see outdated data until TTL expires

### High: Contract Coverage Gaps (flagged by ARCHON)
- `memcached.Task implements service.TaskSearchRepository` — **no contract test**
- `elasticsearch.Task implements memcached.Datastore` — **no contract test**
- Interface compliance is compile-time only; behavioral correctness is unverified

### Medium: Error Handling Inconsistencies
- `Set()` errors are silently swallowed (line 236-240 in diff); failed cache writes degrade silently but are not logged
- Non-`ErrCacheMiss` errors from `client.Get()` propagate as `ErrorCodeUnknown` — consider distinguishing transient network failures

### Medium: Key Construction Fragility
- `newKey()` uses string concatenation with underscore separator: `description_priority_isDone_from_size`
- Risk of key collisions if description contains underscores or numeric-looking strings
- No escaping or hashing; very long descriptions could exceed Memcached's 250-byte key limit

### Low: Hardcoded Configuration
- Cache TTL (25s), client timeout (100ms), max idle conns (100) are hardcoded
- Recommend extracting to configuration for tuning without redeployment

### Low: Expiration Timestamp Bug
- `Expiration: int32(time.Now().Add(25 * time.Second).Unix())` sets an **absolute Unix timestamp**
- Memcached interprets values > 30 days (2592000) as Unix timestamps, values <= as relative seconds
- Setting Unix epoch ~1.7B as expiration works but is unconventional; using `25` (relative seconds) is clearer and safer

---

## Questions for Author

1. Is eventual consistency (up to 25s stale reads) acceptable for this use case, or should writes invalidate relevant cache keys?
2. Should `Set()` failures be logged/monitored to detect cache unavailability?
3. Are contract tests planned for the new `Datastore` interface?

---

## Verdict

The architecture is sound for a read-heavy, eventual-consistency-tolerant workload. **Primary concern**: cache invalidation is absent, creating a 25-second staleness window after writes. Recommend either (a) accepting this as documented behavior, or (b) adding cache-busting on `Index`/`Delete`. The ARCHON-flagged interface coverage gaps should be addressed before merge to prevent silent contract drift.
