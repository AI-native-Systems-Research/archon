# Architectural Review: Cache-Aside Pattern for Task Search

## Summary

This PR introduces a **Memcached-based cache-aside layer** for the Task search functionality. The cache wraps the existing Elasticsearch-backed search and is injected into the service layer.

---

## Architectural Changes

### New Components
- `cmd/internal/memcached.go` - Factory for Memcached client initialization
- `internal/memcached/memcached.go` - Cache-aside wrapper implementing the `Datastore` interface

### New Dependencies
- `github.com/bradfitz/gomemcache` - Memcached client library
- New infrastructure: Memcached service in docker-compose

### Changed Contracts/Interfaces
- `serverConfig` struct extended with `Memcached` field
- Service layer now receives `memcached.Task` instead of `elasticsearch.Task` for the search dependency
- A new `Datastore` interface is introduced to abstract the underlying search backend

### Wiring Changes
- `rest-server/main.go`: Memcached client instantiated at startup and wired through the dependency graph
- The search path is now: `service.Task` -> `memcached.Task` (cache) -> `elasticsearch.Task` (origin)

---

## Architectural Risks and Review Flags

### Critical: Cache Invalidation Is Missing
- **Index() and Delete() pass through without cache invalidation** - When a task is indexed or deleted via `t.orig.Index()` / `t.orig.Delete()`, the cache is NOT invalidated
- Stale reads are guaranteed: a modified/deleted task will still appear in cached search results for up to 25 seconds
- This violates cache-aside correctness; cache should be invalidated on writes

### High: No Tests Included
- Zero test coverage for the new `internal/memcached` package
- The cache key generation logic (`newKey`) and gob encoding/decoding are untested
- Edge cases (nil pointers in SearchArgs, empty results, encoding failures) have no coverage

### High: Silent Failure on Cache Set
- `t.client.Set()` errors are silently ignored (line 236-240)
- If cache writes consistently fail, the system degrades to always hitting Elasticsearch without any observability
- At minimum, log cache write failures

### Medium: TTL Calculation Bug
- `Expiration: int32(time.Now().Add(25 * time.Second).Unix())` is using **absolute Unix timestamp**, but `memcache.Item.Expiration` expects **seconds from now** for values <= 30 days
- This will result in items expiring immediately or having incorrect TTL
- Should be: `Expiration: 25`

### Medium: Hardcoded Configuration
- Cache TTL (25s), connection timeout (100ms), and max idle connections (100) are hardcoded
- No ability to tune these per environment

### Low: Typo in Log Message
- `"settin value"` should be `"setting value"` (line 233)

### Low: Key Generation May Cause Collisions
- `newKey()` uses string concatenation with underscores - edge cases where description contains underscores could theoretically cause key collisions
- Consider hashing or using a more robust key format

---

## Recommendations

1. **Implement cache invalidation** in `Index()` and `Delete()` methods - this is a correctness issue
2. **Fix the TTL calculation** - use relative seconds, not absolute timestamp
3. **Add unit tests** for the memcached wrapper, especially around key generation and error paths
4. **Log or metric cache write failures** instead of silently ignoring them
5. **Externalize configuration** for TTL and connection pool settings
