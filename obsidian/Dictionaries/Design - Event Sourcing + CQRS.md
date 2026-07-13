# Event Sourcing + JetStream + KV Design

## Overview

This design combines three NATS components to build a durable, scalable event sourcing system:

- **JetStream Streams** = System of record (immutable event log)
- **JetStream Consumers** = State tracking for event handlers
- **NATS KV Stores** = Snapshot storage for fast rehydration

## Architecture Diagram

```
┌─────────────────────────────────────────────────┐
│ Your Services (Command Handlers)                │
│ - Load snapshots from KV                        │
│ - Replay events since snapshot sequence         │
│ - Validate business logic & emit new events     │
└────────────────────┬────────────────────────────┘
                     │
                     ▼
        ┌────────────────────────────┐
        │  NATS JetStream Streams    │
        │  (System of Record)        │
        │  - All events stored       │
        │  - Immutable append-only   │
        │  - Per-entity event log    │
        └─────────┬──────────────────┘
                  │
        ┌─────────┴──────────┐
        │                    │
        ▼                    ▼
    JetStream Consumers   KV Buckets
    - Event Handlers     - Snapshots
    - Side effects       - State cache
    - Track position     - Sequence tracking
```

## Components

### 1. JetStream Streams (Event Store)

**Purpose:** Store all events durably and immutably

**Configuration:**
- One stream per domain or entity type (e.g., `events-users`, `events-orders`)
- Or one shared stream partitioned by subject (e.g., `events.users.>`, `events.orders.>`)
- Retention policy: duration-based or size-based depending on requirements

**Guarantees:**
- Append-only (events can never be modified)
- Durable to disk (survives hard restarts)
- Ordered by sequence number per stream

### 2. JetStream Consumers (Event Processing)

**Purpose:** Track which events have been processed and enable replay

**Types:**
- **Durable consumers** for stateful event handlers (email service, billing, etc.)
  - Survive restarts
  - Resume from last processed sequence
  - Share work across instances
- **Ephemeral consumers** for temporary replay or debugging

**Tracking:**
- Each consumer maintains its own read position (sequence number)
- Server tracks acknowledgments automatically
- Re-delivery policy for failed messages

**Configuration:**
```javascript
// Example: Create a durable consumer for order processing
await jsm.consumers.add('events-orders', {
  durable_name: 'order-processor',
  ack_policy: AckPolicy.Explicit,
  ack_wait: Duration.millis(30000),
  max_deliver: 3,
})
```

### 3. KV Buckets (Snapshots)

**Purpose:** Store entity state snapshots to avoid replaying all events

**Structure:**

```typescript
interface Snapshot {
  entityId: string
  state: object           // Current entity state
  sequence: number        // Last event sequence included in state
  timestamp: number       // When snapshot was created
}
```

**KV Bucket Design:**
- Bucket name: `snapshots` (configurable)
- Key format: `{entityType}:{entityId}` (e.g., `user:123`, `order:456`)
- Value: JSON stringified snapshot

**Durability:**
- NATS KV is backed by JetStream internally
- Data persists to disk by default
- Survives hard restarts

## Workflow: Rehydration (Loading Entity State)

```
Request to load entity state
         │
         ▼
Try to load snapshot from KV
         │
    ┌────┴─────┐
    │          │
  Found     Not Found
    │          │
    ▼          ▼
 Load      Start from
 state     beginning
 from      of stream
snapshot       │
    │          ▼
    │   Fetch all events
    │   for this entity
    │          │
    └──────────┘
         │
         ▼
  Replay events from
(snapshot.sequence + 1)
     to latest
         │
         ▼
Return entity state
```

### Code Example: Rehydration

```typescript
async function rehydrateEntity(entityId: string) {
  const kv = jetstream.kv('snapshots')
  
  // Try to load snapshot
  let snapshot = null
  try {
    const snapshotData = await kv.get(`user:${entityId}`)
    snapshot = JSON.parse(snapshotData.string())
  } catch {
    // Snapshot not found, will replay from start
  }
  
  // Determine where to start replaying
  let state = snapshot?.state ?? initialState()
  let fromSequence = (snapshot?.sequence ?? 0) + 1
  
  // Fetch and replay events since snapshot
  const events = await jetstream.consumers.get('events-users', 'replay')
    .fetch({ 
      start_sequence: fromSequence,
      filter_subject: `events.users.${entityId}`
    })
  
  for await (const msg of events) {
    state = applyEvent(state, msg.json())
    msg.ack()
  }
  
  return state
}
```

## Workflow: Creating Snapshots

**When to snapshot:**
- Every N events (e.g., every 100 events)
- On a time interval (e.g., hourly)
- Before/after significant operations
- When rehydration time exceeds threshold

### Code Example: Snapshot Creation

```typescript
async function createSnapshot(entityId: string, state: object, sequence: number) {
  const kv = jetstream.kv('snapshots')
  
  const snapshot = {
    entityId,
    state,
    sequence,
    timestamp: Date.now()
  }
  
  await kv.put(`user:${entityId}`, JSON.stringify(snapshot))
}
```

## Event Design Principles

From [[Event Sourcing pattern]]:

1. **Capture Business Intent** — Events should describe *what happened* and *why*, not just state changes
   - ✅ `UserEmailChanged` with reason and old/new values
   - ❌ `UserUpdated` with generic field change

2. **Immutable Events** — Events are permanent records
   - Fix mistakes with compensating events: `UserEmailRejected` reverses `UserEmailChanged`
   - Never edit or delete historical events

3. **Event Versioning** — As events evolve, version them to handle schema changes
   - Include version in event type or metadata
   - Use tolerant deserialization (ignore unknown fields)
   - Use upcasters to transform old schemas during replay

## Key Design Decisions

| Decision | Choice | Rationale |
| --- | --- | --- |
| System of Record | JetStream only | No external event store; keeps everything in NATS |
| Snapshot Storage | NATS KV | Stays within NATS ecosystem; durable; simple API |
| Consumer Type | Durable | Survives restarts; tracks state automatically |
| Snapshot Frequency | TBD | Balance between replay time and storage cost |
| Event Retention | TBD | Depends on audit/compliance requirements |

## Important Constraints & Guarantees

### Durability
- ✅ JetStream events are durable to disk (persistent storage)
- ✅ KV snapshots are durable (backed by JetStream)
- ✅ Consumer state is durable (server-side)

### Consistency
- ⚠️ **Eventual consistency**: New events may not appear in snapshots immediately
- Events are consistent within a stream (ordered by sequence)
- Different streams may be temporarily out of sync

### Ordering
- ✅ Events within a stream are ordered by sequence number
- ✅ Events for the same entity are ordered
- ⚠️ Events across different entity types may not be globally ordered

### Scalability
- ✅ Per-entity streams scale well (natural partitioning by entity ID)
- ✅ Multiple consumers can process same stream independently
- ✅ Consumer state is server-side (not application-local)

## Testing Strategy

From [[Event Sourcing pattern]], use a "given-when-then" approach:

```typescript
describe('User registration', () => {
  it('should accept valid email', async () => {
    // Given: user creation events
    const givenEvents = [
      { type: 'UserCreated', userId: '123', email: 'old@example.com' }
    ]
    
    // When: user changes email
    const command = { type: 'ChangeEmail', newEmail: 'new@example.com' }
    const newEvents = handleCommand(command, givenEvents)
    
    // Then: event should be emitted
    expect(newEvents).toContainEqual({
      type: 'EmailChanged',
      userId: '123',
      newEmail: 'new@example.com'
    })
  })
})
```

## Common Pitfalls to Avoid

1. **Large event streams without snapshots** → Rehydration becomes slow
   - Solution: Implement snapshot strategy early

2. **Forgetting to version events** → Schema changes break replay
   - Solution: Include version in every event, plan versioning strategy upfront

3. **Treating snapshots as system of record** → Lose audit trail if snapshot corrupted
   - Solution: Always treat events as authoritative; snapshots are just caches

4. **Lost consumer state** → Duplicate processing or missed events
   - Solution: Use durable consumers; verify ack policies

5. **Unidempotent event handlers** → Duplicate events cause duplicates
   - Solution: Design handlers to be idempotent; track processed event IDs

## References

- [[Event Sourcing pattern]] — Pattern overview and guidelines
- [[JetStreams]] — NATS JetStream concepts and consumer design
- [NATS Documentation](https://docs.nats.io/) — Official docs
- [NATS by Example](https://natsbyexample.com/) — Working examples
