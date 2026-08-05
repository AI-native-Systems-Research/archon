# Architectural Review: Kafka Migration PR

## Summary

This PR replaces RabbitMQ with Apache Kafka as the message broker for task events. The REST server now publishes to Kafka, and a new Kafka-based elasticsearch-indexer consumes those events.

---

## Architectural Changes

### New Components
- **`cmd/elasticsearch-indexer-kafka/main.go`** - New consumer service for Kafka-based indexing
- **`cmd/internal/kafka.go`** - Shared Kafka producer/consumer configuration
- **`internal/kafka/task.go`** - Domain-layer Kafka publisher with OpenTelemetry tracing
- **`build/elasticsearch-indexer-kafka/Dockerfile`** - New container for Kafka indexer
- **`build/elasticsearch-indexer-rabbitmq/Dockerfile`** - Preserved RabbitMQ indexer build

### New Dependencies
- `github.com/confluentinc/confluent-kafka-go v1.7.0` (CGO-based, requires librdkafka)
- Zookeeper container added to docker-compose
- Kafka container (wurstmeister/kafka:2.13-2.7.0) added

### Changed Contracts/Interfaces
- `newServer()` signature changed from positional args to `serverConfig` struct
- REST server now instantiates `kafka.NewTask()` instead of `rabbitmq.NewTask()`
- Message broker interface unchanged (Created/Deleted/Updated methods)

### Infrastructure Changes
- Build switches from `CGO_ENABLED=0` to `CGO_ENABLED=1` with static linking
- New env vars: `KAFKA_HOST`, `KAFKA_TOPIC`

---

## Architectural Risks and Review Flags

### Critical

- **No tests for new Kafka code** - Neither `cmd/internal/kafka.go`, `internal/kafka/task.go`, nor the consumer in `elasticsearch-indexer-kafka/main.go` have corresponding test files in the diff. The message broker is a critical path for data consistency.

- **Producer does not wait for delivery confirmation** - `t.producer.Produce()` in `internal/kafka/task.go` uses async delivery (passes `nil` as delivery channel). Messages may be lost silently if the broker rejects them or the producer buffer overflows. The REST API would return success while the message never reaches Kafka.

### High

- **Consumer commits before processing outcome is verified** - In `ListenAndServe()`, on JSON decode error the message is committed (`commit(msg)`), but on Index/Delete failure the message is NOT committed, leading to reprocessing. However, the decode-error-commit path silently drops malformed messages with no dead-letter handling.

- **No dead-letter queue or poison message handling** - Invalid messages are logged and committed, lost forever. No mechanism to inspect or reprocess failed events.

- **Graceful shutdown race condition** - The consumer poll loop checks `s.closeC` in a select with `default`. If `closeC` is closed while `Poll(150)` is blocked, the message in flight could be partially processed before shutdown completes.

### Medium

- **Hardcoded poll timeout** - `Poll(150)` uses a magic number. Should be configurable for tuning consumer responsiveness.

- **KAFKA_HOST validation missing** - `newKafkaConfig()` validates `KAFKA_TOPIC` is non-empty but allows empty `KAFKA_HOST`, which would cause a runtime failure.

- **Commented-out RabbitMQ code left in** - Significant blocks of commented code in `cmd/rest-server/main.go` and `docker-compose.yml` create maintenance burden. Either complete the removal or use feature flags.

- **No consumer offset monitoring** - No metrics or health checks for consumer lag. A stuck consumer would go undetected.

### Low

- **Topic auto-creation reliance** - `KAFKA_AUTO_CREATE_TOPICS_ENABLE: 'true'` and `KAFKA_CREATE_TOPICS: 'tasks:1:1'` in docker-compose. Production deployments typically pre-create topics with proper partition counts and replication factors.

- **Single partition** - Topic created with `tasks:1:1` (1 partition, 1 replica). This limits consumer parallelism and provides no fault tolerance.

---

## Boundary Concerns

- **Interface parity not enforced** - The `rabbitmq.Task` and `kafka.Task` both implement the same implicit interface, but no explicit interface type is defined. A shared interface would catch method signature drift.

- **No integration test for end-to-end flow** - The REST -> Kafka -> Elasticsearch pipeline has no visible integration test. This is the critical data path.

---

## Verdict

The PR successfully abstracts the message broker swap, preserving the service interface. However, **message delivery reliability is compromised** by async produce without delivery confirmation, and **the entire Kafka layer lacks test coverage**. These should be addressed before merge.
