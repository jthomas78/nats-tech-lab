---
adr: 53
title: Use Shared PostgreSQL Instances by Default
status: Accepted
date: 2026-09-03
scope: v3
context: data
decision: Share PostgreSQL infrastructure by default within a region. Give every service its own logically isolated database, credentials and migrations. Move a database to dedicated infrastructure only on a demonstrated requirement.
why: Dedicated instances per database cost more, fragment capacity and multiply operational work. Logical isolation by database, role and privileges keeps service ownership strong without that cost.
related: [52]
applied_by: [52]
---

# ADR-053: Use Shared PostgreSQL Instances by Default

**Status:** **Accepted 2026-09-03**
**Decision Type:** Infrastructure / Data Architecture
**Scope:** Proposed Linebooker V3 Architecture (platform principle, not a lab implementation)
**Date:** 2026-09-03
**Deciders:** Jeremy (repo owner)
**Related:** [ADR-052](ADR-052-lab-data-one-postgres-instance-database-per-service.md) applies this principle to the tech-lab compose stack, and records the concrete enforcement mechanism (`REVOKE CONNECT ... FROM PUBLIC`, `GRANT CONNECT` to the owning role, per-role `CONNECTION LIMIT`); [ARCHITECTURE-ACCOUNTS.md](../Dictionary-POC/ARCHITECTURE-ACCOUNTS.md) (in the lab, tenancy is the NATS account boundary, which is why the "Tenant Considerations" section below is a separate axis)

## Context

The Linebooker platform consists of multiple services and bounded domains that require PostgreSQL persistence.

There are two primary infrastructure approaches for hosting these databases:

1. Provision a dedicated PostgreSQL instance for each database.
2. Host multiple logically separate PostgreSQL databases on a shared PostgreSQL instance.

For example, four service databases could be deployed using four independent PostgreSQL instances:

```text
PostgreSQL Instance 1
└── Organisation Database

PostgreSQL Instance 2
└── Fleet Database

PostgreSQL Instance 3
└── Shipping Database

PostgreSQL Instance 4
└── Marketplace Database
```

Alternatively, the databases can remain logically separate while sharing the same PostgreSQL infrastructure:

```text
Shared PostgreSQL Instance
│
├── organisation_db
├── fleet_db
├── shipping_db
└── marketplace_db
```

Provisioning a separate PostgreSQL instance for every database creates additional infrastructure cost and operational overhead.

It can also result in inefficient resource utilisation, particularly where individual services have relatively small or variable workloads.

A shared PostgreSQL instance allows CPU, memory, connections, storage throughput, and I/O capacity to be pooled across multiple logically independent databases.

## Decision

Linebooker will use **shared PostgreSQL instances as the default PostgreSQL deployment model**.

Multiple service or domain databases may reside on the same PostgreSQL instance while remaining logically isolated as independent databases.

The preferred regional deployment model is:

```text
Region
│
└── PostgreSQL Instance / Cluster
    │
    ├── organisation_db
    ├── fleet_db
    ├── shipping_db
    ├── marketplace_db
    └── other service databases
```

The PostgreSQL infrastructure may be provided through:

* a managed cloud database service;
* a hosted PostgreSQL platform;
* containerised PostgreSQL;
* virtual-machine-based PostgreSQL;
* Kubernetes-based PostgreSQL;
* or another suitable deployment platform.

The ADR intentionally does not mandate a specific cloud provider or PostgreSQL hosting product.

## Database Ownership

Sharing PostgreSQL infrastructure does **not** imply sharing application data ownership.

Database ownership remains aligned with service and domain boundaries.

For example:

```text
Organisation Service ──→ organisation_db
Fleet Service        ──→ fleet_db
Shipping Service     ──→ shipping_db
Marketplace Service  ──→ marketplace_db
```

Each service should use dedicated database credentials and should only have access to the database or databases it owns.

Services must not bypass service boundaries by directly querying databases owned by another service.

Cross-domain communication should occur through defined interfaces such as:

* service APIs;
* request/reply messaging;
* domain events;
* integration events;
* or workflow contracts.

## Rationale

### Cost Efficiency

The primary reason for this decision is infrastructure efficiency.

For four databases, the dedicated model requires four independently provisioned PostgreSQL environments:

```text
Dedicated model

Database A → PostgreSQL Instance A
Database B → PostgreSQL Instance B
Database C → PostgreSQL Instance C
Database D → PostgreSQL Instance D
```

Whereas the shared model requires one infrastructure environment:

```text
Shared model

PostgreSQL Instance
├── Database A
├── Database B
├── Database C
└── Database D
```

With managed database platforms, compute resources are generally provisioned and charged at the instance or cluster level.

With self-managed PostgreSQL, the same principle applies operationally: every additional instance requires additional compute, memory, storage, monitoring, backup, maintenance, and operational capacity.

A shared instance may need to be larger than an instance serving a single database, but consolidation generally allows resources to be used more efficiently.

## Resource Pooling

Independent databases are unlikely to reach peak CPU, memory, connection, and I/O utilisation simultaneously.

A shared PostgreSQL instance allows available capacity to be used across multiple workloads.

This reduces infrastructure fragmentation and improves overall utilisation.

For example:

```text
Dedicated

Instance A    Instance B    Instance C    Instance D
[ 25% ]       [ 10% ]       [ 40% ]       [ 15% ]

Unused capacity exists independently on every instance.
```

Compared with:

```text
Shared

PostgreSQL Cluster
[ Combined workload across A + B + C + D ]
```

The shared model allows capacity to be provisioned against the aggregate workload rather than independently against each service's potential peak.

## Reduced Operational Overhead

A smaller number of PostgreSQL instances reduces the number of infrastructure resources that must be:

* provisioned;
* configured;
* monitored;
* patched;
* upgraded;
* secured;
* backed up;
* replicated;
* restored;
* capacity planned;
* and maintained.

This is particularly valuable during the early and intermediate growth stages of the platform.

## Preserved Logical Isolation

Sharing PostgreSQL infrastructure does not prevent strong logical boundaries.

Separate PostgreSQL databases, database users, permissions, migrations, and ownership rules can preserve service-level isolation.

The intended model is:

```text
Service
   │
   │ dedicated credentials
   ▼
Owned Database
   │
   X
   │ no direct access
   ▼
Other Service Database
```

Each service should therefore have:

* an independently named database;
* dedicated database credentials;
* independently managed migrations;
* explicit ownership;
* appropriate connection limits;
* and restricted privileges.

## Consequences

### Positive

The decision provides:

* lower baseline infrastructure cost;
* improved compute and memory utilisation;
* fewer PostgreSQL environments to operate;
* simplified backup and monitoring infrastructure;
* easier provisioning of new service databases;
* preservation of logical database ownership;
* reduced idle infrastructure;
* and the ability to scale vertically before introducing additional PostgreSQL instances.

### Negative

Databases sharing the same PostgreSQL infrastructure also share finite resources such as:

* CPU;
* memory;
* connection limits;
* disk throughput;
* IOPS;
* storage capacity;
* maintenance operations;
* and potentially the same infrastructure failure domain.

A resource-intensive workload can therefore affect other databases hosted on the same instance.

The shared infrastructure also creates a larger blast radius than completely independent PostgreSQL instances.

## Isolation Requirements

The following rules apply regardless of whether databases share physical infrastructure.

### Service-Level Ownership

Each database must have a clearly identified owning service or domain.

### Dedicated Credentials

Services must use dedicated PostgreSQL users or credentials.

Credentials must be scoped according to the principle of least privilege.

### No Cross-Service Database Access

A service must not directly query or modify another service's database.

For example:

```text
Fleet Service ────────→ fleet_db        ✓

Fleet Service ────────→ shipping_db     ✗
```

If Fleet requires Shipping information:

```text
Fleet Service
      │
      ├── API / Request-Reply
      │
      └── Event / Projection
               │
               ▼
        Shipping Service
```

The database must not become an unofficial integration mechanism between services.

## Scaling Strategy

Shared PostgreSQL instances are the **default starting position**, not a requirement that every database remain consolidated permanently.

A database may be migrated to a dedicated PostgreSQL instance or cluster when there is a demonstrated requirement.

### Performance

A service consistently consumes a significant percentage of shared:

* CPU;
* memory;
* storage throughput;
* IOPS;
* or connections.

### Noisy-Neighbour Effects

One workload materially affects the latency, throughput, or reliability of other workloads.

### Independent Scaling

A database has substantially different capacity or growth characteristics from the other databases.

For example:

```text
Shared PostgreSQL
├── organisation_db      small
├── billing_db           small
├── marketplace_db       medium
└── tracking_db          very high write volume
```

The tracking database may eventually justify independent infrastructure:

```text
Shared PostgreSQL
├── organisation_db
├── billing_db
└── marketplace_db

Dedicated PostgreSQL
└── tracking_db
```

### Availability Requirements

A service may require different:

* availability targets;
* replication strategies;
* recovery objectives;
* backup policies;
* maintenance windows;
* or disaster recovery characteristics.

### Security or Regulatory Requirements

A tenant, region, customer, or service may require stronger infrastructure-level isolation.

Examples include:

* regulatory separation;
* contractual data isolation;
* data residency;
* customer-specific infrastructure;
* or stricter security boundaries.

### Operational Independence

A database may require:

* a different PostgreSQL version;
* specialised PostgreSQL extensions;
* specific configuration;
* different maintenance windows;
* or independently managed lifecycle policies.

## Evolution Model

The architecture follows a:

> **Consolidate first, separate when justified**

model.

Initial state:

```text
Shared PostgreSQL
├── DB A
├── DB B
├── DB C
└── DB D
```

As workloads evolve:

```text
                       scaling /
                       isolation /
                       availability
                       requirement
                            │
                            ▼

Shared PostgreSQL
├── DB A
├── DB B
└── DB D

Dedicated PostgreSQL
└── DB C
```

This allows infrastructure isolation to increase as platform requirements become clear without paying the operational and financial cost of dedicated infrastructure prematurely.

## Multi-Region Considerations

The shared-instance decision applies independently within each deployment region.

For example:

```text
South Africa Region
│
└── PostgreSQL Cluster
    ├── organisation_db
    ├── fleet_db
    ├── shipping_db
    └── marketplace_db


Europe Region
│
└── PostgreSQL Cluster
    ├── organisation_db
    ├── fleet_db
    ├── shipping_db
    └── marketplace_db
```

Databases should not automatically be consolidated across regions where doing so would compromise:

* data residency;
* latency;
* regional autonomy;
* fault isolation;
* regulatory requirements;
* or disaster recovery strategy.

The architectural unit is therefore generally:

> **Shared PostgreSQL infrastructure within a deployment region, with logically isolated databases per service or domain.**

## Tenant Considerations

This decision is independent of the tenant isolation strategy.

A shared PostgreSQL instance may host databases supporting:

* multiple tenants in shared tables using a tenant identifier;
* schema-per-tenant models;
* database-per-tenant models;
* database-per-service models;
* or combinations of these approaches.

For example:

```text
Regional PostgreSQL
│
├── organisation_db
│   ├── Tenant A
│   ├── Tenant B
│   └── Tenant C
│
├── fleet_db
│   ├── Tenant A
│   ├── Tenant B
│   └── Tenant C
│
└── shipping_db
    ├── Tenant A
    ├── Tenant B
    └── Tenant C
```

Tenant isolation and PostgreSQL infrastructure isolation should therefore be treated as separate architectural concerns.

A requirement for tenant isolation does not automatically imply a requirement for a PostgreSQL instance per tenant.

## Alternatives Considered

### Dedicated PostgreSQL Instance per Database

**Rejected as the default approach.**

Advantages:

* maximum infrastructure isolation;
* independent resource allocation;
* independent scaling;
* reduced database-level blast radius;
* independent maintenance;
* easier workload-specific tuning.

Disadvantages:

* higher baseline infrastructure cost;
* greater operational complexity;
* fragmented compute capacity;
* increased monitoring and backup infrastructure;
* larger infrastructure footprint;
* inefficient utilisation for small workloads.

Dedicated PostgreSQL instances remain an accepted exception where justified by workload, security, regulatory, availability, or operational requirements.

### Single Database with Schemas per Service

Also considered:

```text
PostgreSQL Instance
└── linebooker
    ├── organisation_schema
    ├── fleet_schema
    ├── shipping_schema
    └── marketplace_schema
```

This provides even greater infrastructure consolidation but creates a weaker logical boundary between services.

The preferred model is:

```text
PostgreSQL Instance
│
├── organisation_db
├── fleet_db
├── shipping_db
└── marketplace_db
```

Separate databases provide clearer:

* ownership;
* permissions;
* migration boundaries;
* connection configuration;
* lifecycle management;
* and future extraction paths.

## Provider Independence

This ADR defines an architectural principle rather than a cloud implementation.

Possible implementations include:

```text
Managed Cloud PostgreSQL
        │
        ├── AWS
        ├── Azure
        ├── Google Cloud
        ├── DigitalOcean
        ├── Scaleway
        └── other providers

Self-Managed PostgreSQL
        │
        ├── Virtual Machines
        ├── Containers
        ├── Kubernetes
        └── Bare Metal
```

The provider or deployment technology may change without changing the architectural decision.

Provider-specific deployment details should therefore be documented separately in infrastructure design documents or provider-specific ADRs.

## Decision Summary

Linebooker will **default to shared PostgreSQL instances or clusters within each deployment region**.

Multiple service databases may share the same PostgreSQL infrastructure while retaining independent database ownership and access controls.

Dedicated PostgreSQL infrastructure should be introduced only where measurable requirements justify it.

These requirements may include:

* workload scale;
* noisy-neighbour effects;
* security;
* regulatory isolation;
* availability;
* independent scaling;
* specialised PostgreSQL configuration;
* or operational independence.

### Architectural Principle

> **Share PostgreSQL infrastructure by default, isolate databases logically, and separate infrastructure only when there is a demonstrated requirement to do so.**
