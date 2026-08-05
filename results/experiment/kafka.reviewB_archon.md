# Architectural Review: Kafka Message Broker Migration

## Summary of Changes

This PR migrates the message broker infrastructure from RabbitMQ to Kafka across the todo-api system.

## New Components (ARCHON confirmed)

- **`cmd/elasticsearch-indexer-kafka`** - New Kafka-based indexer consumer service
- **`cmd/elasticsearch-indexer-rabbitmq`** - Renamed from `cmd/elasticsearch-indexer` (preserved for dual-broker support)
- **`internal/kafka`** - New Kafka adapter package implementing the message broker interface
- **`cmd/internal/kafka.go`** - Kafka producer/consumer factory functions

## New External Dependencies

- **`github.com/confluentinc/confluent-kafka-go/kafka`** - Confluent Kafka Go client (CGO-based)
- **service:Kafka** - New infrastructure dependency (Zookeeper + Kafka containers in docker-compose)

## Changed Contracts

- **`internal/kafka.Task`** now implements `service.TaskMessageBrokerRepository`
  - Methods: `Created()`, `Deleted()`, `Updated()`
- **`rest-server`** switched from `rabbitmq.NewTask()` to `kafka.NewTask()` as its message broker

## Architectural Risks and Gaps

### Critical: Contract Test Coverage Gap

- **ARCHON flags**: `kafka.Task` implements `TaskMessageBrokerRepository` but **no contract test guards this interface**
- The same gap exists for `rabbitmq.Task` - neither implementation has contract-level test coverage
- **Risk**: Behavioral drift between implementations; the interface contract is enforced only by the compiler, not by runtime behavior tests

### Build Complexity Increase

- **CGO now required**: `CGO_ENABLED=1` for all Kafka-linked binaries
- Rest-server, elasticsearch-indexer-kafka all now require `librdkafka-dev` and CGO toolchain
- **Risk**: Increased build time, larger images, potential cross-compilation issues

### Incomplete Migration (Commented Code)

- RabbitMQ code is commented out but not removed in `cmd/rest-server/main.go`
- `serverConfig` struct still has `RabbitMQ` field though unused
- **Risk**: Dead code accumulation; unclear if dual-broker support is intentional

### Error Handling Consistency

- Kafka producer uses fire-and-forget (`Produce()` with nil delivery channel) - no confirmation of message delivery
- RabbitMQ consumer had explicit message acknowledgment; Kafka indexer commits after processing but **does not retry on failure**
  - Lines 277-287: On index/delete failure, message is NOT committed but also NOT retried
  - **Risk**: Messages silently dropped on transient ES failures

### Serialization Format Change

- Kafka uses JSON (`encoding/json`) for message serialization
- RabbitMQ used GOB (`encoding/gob`)
- **Risk**: Not backward compatible; cannot roll back without format translation

### Missing Infrastructure Configuration

- Kafka topic auto-creation enabled (`KAFKA_AUTO_CREATE_TOPICS_ENABLE: 'true'`)
- Single partition, single replica (`tasks:1:1`)
- **Risk**: No durability guarantees in current config; not production-ready

## Reviewer Checklist

1. [ ] Confirm intentional switch from GOB to JSON serialization
2. [ ] Decide: keep or remove RabbitMQ code paths
3. [ ] Add contract tests for `TaskMessageBrokerRepository` interface
4. [ ] Add delivery confirmation for Kafka producer (handle delivery reports)
5. [ ] Implement retry logic for transient ES failures in consumer
6. [ ] Review Kafka partition/replication strategy for production
