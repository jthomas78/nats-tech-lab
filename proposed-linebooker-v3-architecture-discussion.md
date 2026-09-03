# Proposed Linebooker V3 Architecture Discussion

## User

What should a multi-region and multi-tenant trucking logistics app cover in terms of architectural design and diagrams. For starters I do think there should be a system or overview level of the archictecture, and that it expands on individual areas (feature - functional and non-functional) on subsequent or derived diagrams

## Assistant

Yes — that is the right way to approach it.

For a **multi-region, multi-tenant trucking/logistics platform**, I would not try to make one enormous architecture diagram. I would create a **hierarchy of diagrams**, where the top-level diagram acts as the map of the entire system and each major box can be expanded into one or more derived diagrams.

A useful mental model is:

```text
                    ARCHITECTURE OVERVIEW
                            │
          ┌─────────────────┼─────────────────┐
          │                 │                 │
      Business          Functional       Non-Functional
      Context            Architecture     Architecture
          │                 │                 │
          │        ┌────────┼────────┐        │
          │        │        │        │        │
      Actors     Orders   Fleet   Tracking   Security
      Orgs       Trips    Docs    Billing    Scaling
      Markets    Tender   etc.              DR / HA
                                             │
                                      Deployment / Regions
```

The **overview diagram should tell the story of the platform**, while the derived diagrams explain how individual areas work.

## 1. Start with a System / Platform Overview

This should probably be the first diagram anyone sees.

It should answer:

> What is this platform, who participates in it, what are its major capabilities, and what are the major technical building blocks?

For your type of logistics platform, I'd expect something approximately like:

```text
                        LOGISTICS PLATFORM
                              │
        ┌─────────────────────┼──────────────────────┐
        │                     │                      │
 Platform Admins          Operators              Participants
                              │                      │
                    ┌─────────┴─────────┐       ┌────┴──────────┐
                    │                   │       │               │
                 ZA Market          AU Market  Customers    Transporters
                                                     │          │
                                                   Users      Users
                                                              Drivers
                              │
                              ▼
                    APPLICATION PLATFORM
                              │
                ┌─────────────┴─────────────┐
                │                           │
           Web / Mobile                  APIs
                │
        ┌───────┴────────┐
        │ Application    │
        │ Shell / MFE    │
        └───────┬────────┘
                │
   ┌────────────┼────────────────────────────────┐
   │            │            │                   │
Marketplace   Orders       Shipping            Fleet
   │            │            │                   │
Tendering     Loads         Trips        Vehicles / Drivers
   │            │            │                   │
   └────────────┴────────────┴───────────────────┘
                        │
                 Domain Services
                        │
              ┌─────────┴─────────┐
              │                   │
             NATS              Temporal
              │                   │
        Events / RPC          Workflows
              │                   │
              └─────────┬─────────┘
                        │
              PostgreSQL / Object Store
                        │
                 Infrastructure
                        │
            ┌───────────┴───────────┐
            │                       │
        Region ZA                Region AU
```

This is deliberately conceptual.

You don't want subjects, ports, Kubernetes pods, database schemas or workflow states on this diagram.

---

## 2. Treat the overview as a navigation map

Every major block should have a corresponding derived view.

For example:

```text
OVERVIEW
│
├── Participants & Tenancy
│
├── Application / MFE Architecture
│
├── Marketplace Architecture
│
├── Orders & Shipping
│
├── Fleet
│
├── Tracking
│
├── Documents
│
├── Billing / Settlement
│
├── NATS Messaging
│
├── Temporal Workflows
│
├── Data Architecture
│
├── Security
│
├── Multi-Region Architecture
│
├── High Availability / DR
│
└── Observability
```

That becomes essentially the **architecture table of contents**.

---

# Recommended architecture diagram set

I'd divide the diagrams into four families.

| Diagram | Main question it answers | Typical content |
|---|---|---|
| **1. Platform Overview** | What is the system? | Users, organisations, operators, applications, major domains, platform, regions |
| **2. Business / Participant Model** | Who participates? | Platform, Operator, Customer, Transporter, Broker, Driver, users, memberships |
| **3. Tenant Architecture** | How is tenant isolation implemented? | Operators, organisations, tenant context, permissions, data isolation |
| **4. Multi-Region Architecture** | Where does the system run? | ZA, AU, EU, NATS clusters, gateways, DBs, services |
| **5. Application Architecture** | What applications exist? | Admin UI, customer portal, transporter portal, mobile apps |
| **6. MFE / UI Architecture** | How does the frontend compose? | Shell, plugins, routes, navigation contributions, manifests |
| **7. Domain Architecture** | What business capabilities exist? | Orders, Trips, Fleet, Marketplace, Tracking, Docs, Billing |
| **8. Service Architecture** | What services implement them? | APIs, domain services, dependencies |
| **9. Messaging Architecture** | How do services communicate? | NATS Core, JetStream, accounts, subjects, exports/imports |
| **10. Workflow Architecture** | How are long-running processes coordinated? | Temporal workflows, activities, events, humans |
| **11. Data Architecture** | Where does state live? | PostgreSQL, JetStream, KV, object store, projections |
| **12. Security Architecture** | Who can access what? | Identity, JWTs, RBAC, org membership, service identity |
| **13. Deployment Architecture** | What runs where? | containers, nodes, clusters, regions, load balancers |
| **14. HA / DR Architecture** | What happens when things fail? | replication, failover, backup, recovery |
| **15. Observability Architecture** | How is the system operated? | metrics, logs, traces, NATS monitoring, Temporal visibility |
| **16. Integration Architecture** | How do external systems connect? | GPS, telematics, ERP, payments, maps, customer integrations |

That sounds like many diagrams, but they don't all need to exist on day one.

The first **five or six** establish the architecture remarkably well.

---

# Functional architecture should be domain-oriented

For the logistics side, I'd avoid organizing the system around technical services initially.

Start with capabilities.

For example:

```text
                         LOGISTICS DOMAIN
                                │
        ┌───────────────────────┼────────────────────────┐
        │                       │                        │
    Commercial              Execution                Assets
        │                       │                        │
    Customers                 Orders                   Fleet
    Contracts                 Loads                    Vehicles
    Pricing                   Trips                    Trailers
    Marketplace               Routes                   Drivers
    Tendering                 Stops                    Equipment
        │                       │                        │
        └───────────────────────┼────────────────────────┘
                                │
                         Supporting Domains
                                │
                  ┌─────────────┼─────────────┐
                  │             │             │
              Documents      Tracking       Billing
              Compliance     Telemetry      Settlement
              Insurance      ETA            Invoicing
```

Then each of those capabilities can have a service architecture underneath it.

For example:

```text
FLEET

Fleet Management
     │
     ├── Vehicle Service
     ├── Driver Service
     ├── Trailer Service
     ├── Maintenance
     ├── Compliance
     └── Availability
```

That's much easier to understand than starting with:

```text
fleet-service-1
fleet-worker
fleet-api
fleet-consumer
fleet-db
```

Those belong in lower-level diagrams.

---

# Separate tenancy from geography

This is particularly important in your architecture.

Do **not** make this:

```text
Region = Tenant
```

a fundamental assumption.

You really have two different dimensions.

### Geography

```text
GLOBAL PLATFORM
      │
 ┌────┼────┐
 │    │    │
ZA   AU   EU
```

This answers:

**Where does workload/data execute?**

### Business tenancy

```text
Operator / Marketplace
          │
     Organisations
      ┌───┴────┐
 Customer   Transporter
    │            │
  Users        Users
```

This answers:

**Who owns/sees/controls data?**

Then you explicitly show their relationship:

```text
                     Organisation A
                          │
                    participates in
                          │
               ┌──────────┴───────────┐
               ▼                      ▼
          ZA Operator             AU Operator

               │                      │
               ▼                      ▼
          ZA Region               AU Region
```

That diagram is extremely valuable because it prevents developers from confusing:

**business boundary → security boundary → deployment boundary → data residency boundary**.

They may align in some deployments, but they are not the same concept.

---

# The participant model deserves its own diagram

For a multi-tenant logistics platform, I would consider this almost as important as the system overview.

Something like:

```text
                           PLATFORM
                              │
                       Platform Admins
                              │
              ┌───────────────┴───────────────┐
              │                               │
        Operator ZA                      Operator AU
              │
         Marketplace
              │
    ┌─────────┼──────────┐
    │         │          │
 Customer  Transporter  Broker
    │         │
    │         ├── Fleet
    │         ├── Vehicles
    │         ├── Drivers
    │         └── Depots
    │
    └── Users

Each Organisation
      │
      ├── Admin
      ├── Operations
      ├── Finance
      └── Other roles
```

And underneath this you can have an actual relationship model:

```text
User
  │
  ▼
OrganisationMembership
  │
  ▼
Organisation
  │
  ▼
OperatorMembership
  │
  ▼
Operator / Market
```

That is the diagram that explains **multi-tenancy** much better than drawing ten database boxes.

---

# Then show the application's functional architecture

This would be a second-level diagram.

```text
┌─────────────────────────────────────────────────────────────┐
│                    LOGISTICS PLATFORM                       │
│                                                             │
│  Marketplace   Orders    Shipping    Fleet     Tracking     │
│                                                             │
│  Documents     Compliance    Billing    Identity            │
│                                                             │
└──────────────────────────────┬──────────────────────────────┘
                               │
                         Platform Services
                               │
         ┌─────────────────────┼─────────────────────┐
         │                     │                     │
       NATS                 Temporal              Storage
```

Then derive diagrams such as:

```text
Marketplace
   ↓
Tender
   ↓
Transporter Matching
   ↓
Capacity
   ↓
Award
```

or:

```text
Order
  ↓
Load
  ↓
Trip
  ↓
Stops
  ↓
POD
  ↓
Invoice
  ↓
Settlement
```

---

# Workflow diagrams are essential in logistics

Logistics contains many **long-lived, stateful processes**, so sequence/state/workflow diagrams become much more valuable than they are in many CRUD systems.

For example:

```text
Customer
   │
   │ Create Load
   ▼
Order Service
   │
   ▼
Tender Workflow
   │
   ├── Find Transporters
   │
   ├── Publish Tender
   │
   ├── Wait for Responses
   │
   ├── Award Transporter
   │
   ▼
Trip Workflow
   │
   ├── Assign Vehicle
   ├── Assign Driver
   ├── Collect
   ├── Track
   ├── Deliver
   ├── POD
   ▼
Settlement
```

Temporal fits naturally into these diagrams because you can distinguish:

```text
EVENT
   ↓
NATS
   ↓
Workflow
   ↓
Temporal
   ↓
Activities / Services
```

rather than presenting Temporal as merely another infrastructure box.

---

# Non-functional architecture should have its own views

This is where I think architecture documentation often goes wrong.

People attempt to put:

- HA
- DR
- security
- scaling
- observability
- networking
- tenancy
- data residency

on the functional architecture diagram.

It becomes unreadable.

Instead, reuse the same system but change the **viewpoint**.

For example, the multi-region view:

```text
                         GLOBAL PLATFORM
                               │
                     Global Control Plane
                               │
            ┌──────────────────┴──────────────────┐
            │                                     │
        ZA REGION                             AU REGION
            │                                     │
      NATS Cluster                           NATS Cluster
            │◄────────── Gateway ────────────────►│
            │                                     │
     Domain Services                        Domain Services
            │                                     │
      PostgreSQL                             PostgreSQL
            │                                     │
     Object Storage                         Object Storage
```

The same system, but now we're explaining **geography, resilience and locality**.

---

# Security should also be its own architecture view

For example:

```text
                     Identity Provider
                           │
                    Authentication
                           │
                           ▼
                          User
                           │
                    Organisation
                           │
                     Membership
                           │
                         Roles
                           │
                ┌──────────┴───────────┐
                │                      │
             UI/API                 NATS
          permissions            permissions
                │                      │
                └──────────┬───────────┘
                           ▼
                      Domain Data
```

Then show separately:

```text
Platform Admin
      │
      └── cross-platform

Operator Admin
      │
      └── operator scope

Organisation Admin
      │
      └── organisation scope

Normal User
      │
      └── role/capability scope
```

This becomes your conceptual authorization architecture.

---

# Data architecture deserves another derived view

I'd make the distinction explicit between **business truth, workflow state, event history and read models**.

For example:

```text
                       Domain Service
                            │
             ┌──────────────┼──────────────┐
             │              │              │
             ▼              ▼              ▼
        PostgreSQL      JetStream       Temporal
         Domain         Events          Workflow
          State                         State
             │              │
             │              ▼
             │          Projections
             │              │
             └──────────────┼──────────────┐
                            ▼              ▼
                          PG Read         KV
                           Models        Cache
```

Then object storage:

```text
Documents
POD
Invoices
Insurance
Compliance Docs
        │
        ▼
    Object Store
```

This becomes extremely useful when discussing event sourcing because it prevents the mistaken idea that **NATS, Temporal and PostgreSQL are competing databases**.

They're serving different architectural purposes.

---

# I would structure your architecture documentation like this

The first page should effectively contain:

```text
                         ARCHITECTURE MAP

                           01 Overview
                               │
       ┌───────────────────────┼────────────────────────┐
       │                       │                        │
  02 Participants         03 Functional            04 Regions
  & Tenancy               Architecture              & Deployment
       │                       │                        │
       ▼                       ▼                        ▼
 Users / Orgs         Domain Capabilities       NATS topology
 Operators            Services                  HA / DR
 Markets              Workflows                 Data residency

                               │
                   ┌───────────┼────────────┐
                   │           │            │
                05 Data     06 Security  07 Operations
                   │           │            │
                  DBs         Auth         Metrics
                 Events       RBAC          Logs
                Storage       JWTs         Traces
```

Then the reader can drill down.

---

## One principle I'd adopt

Every diagram should answer **one primary architectural question**.

If you cannot finish this sentence:

> "This diagram explains ______."

then the diagram is probably trying to show too much.

For example:

**Overview:**  
"This diagram explains what the logistics platform consists of."

**Participant diagram:**  
"This diagram explains who uses and participates in the platform."

**Tenancy diagram:**  
"This diagram explains how organisations are isolated."

**Multi-region diagram:**  
"This diagram explains where workloads execute and how regions communicate."

**NATS diagram:**  
"This diagram explains how messaging is structured."

**Temporal diagram:**  
"This diagram explains how long-running logistics processes are coordinated."

**Deployment diagram:**  
"This diagram explains what actually gets deployed."

That approach will make your V2/V3 architecture documentation significantly easier to maintain than trying to create one canonical mega-diagram.

For your particular platform, I'd probably define the first major architecture set as **Overview → Participant/Tenant Model → Functional Domains → Application/MFE → NATS/Event Architecture → Temporal/Workflow → Multi-Region Deployment → Data → Security → Observability**. Everything more detailed can then hang underneath those views.

---

## User

Does that change how v2 and v3 diagrams are currently setup. There's alot of relevent information but do we need to expand on it a bit more based on your feedback. 

Comments for now.

## Assistant

Yes. I would **expand and slightly reorganise V2/V3**, but I would **not throw away what is already there**. The technical information we developed around regions, NATS, services, Temporal, organisations, etc. is still relevant. The improvement is mainly about **giving each diagram a clearer architectural purpose and introducing a drill-down hierarchy**.

The key change in thinking is:

> **V2/V3 shouldn't try to be “the architecture”. They should be the top of an architecture model from which more focused diagrams are derived.**

### How I'd evolve the current structure

I would make the two diagrams serve different viewpoints rather than simply making V3 a more crowded V2.

| Diagram | Purpose | What it should primarily show |
|---|---|---|
| **V2 – System / Platform Overview** | Explain the entire system to someone new | Actors, operators/markets, organisations, applications, major logistics capabilities, platform, regions |
| **V3 – Logical / Technical Architecture** | Explain how the platform is constructed | Shell/MFE, domain services, NATS, Temporal, data stores, platform services, regional boundaries |
| **Derived diagrams** | Explain one aspect properly | Tenancy, NATS topology, workflows, security, deployment, data, MFE, observability, etc. |

So V2 becomes almost a **map of the architecture documentation**.

---

### V2 should probably become slightly less technical

I think this is the biggest adjustment.

The overview should show things we have only recently made explicit:

```text
                        PLATFORM
                           │
                    Platform Admins
                           │
                  Operators / Markets
                     ZA       AU
                           │
                     Participants
                ┌──────────┴──────────┐
                │                     │
         Customer Organisations   Transporters
                │                     │
              Users                  Users
                                   Drivers
                           │
                           ▼
                    LOGISTICS SYSTEM
                           │
       ┌───────────────────┼────────────────────┐
       │                   │                    │
   Marketplace          Execution             Fleet
       │                   │                    │
 Tendering            Orders / Loads      Vehicles / Drivers
 Matching             Trips / Routes      Compliance
 Capacity             POD / Tracking      Availability
       │                   │                    │
       └───────────────────┼────────────────────┘
                           │
                  Application Platform
                           │
                NATS / Temporal / Data
                           │
                ┌──────────┴─────────┐
                │                    │
              ZA Region           AU Region
```

This communicates far more about **what the system actually is** than an overview dominated by NATS clusters and databases.

The participant hierarchy we already settled on is explicitly **Platform → Operator/Market → Organisation → User**, with Customer, Transporter and potentially Broker/Agent as organisation types.

That belongs visibly in V2.

---

### V3 can retain much more of the existing technical information

I would expect V3 to expand the lower half of V2:

```text
                        APPLICATIONS
                             │
                   Application Shell / MFE
                             │
          ┌──────────────────┼───────────────────┐
          │                  │                   │
      Marketplace          Orders              Fleet
          │                  │                   │
          └──────────────────┼───────────────────┘
                             │
                       Domain Services
                             │
          ┌──────────────────┼───────────────────┐
          │                  │                   │
        NATS              Temporal             Data
          │                  │                   │
       Events             Workflows        PostgreSQL
       Commands           Activities       Object Store
       RPC                                  Projections
                             │
                    Platform Services
                             │
                 Identity / Observability
                             │
             ┌───────────────┴───────────────┐
             │                               │
         ZA Region                       AU Region
```

But there is one important thing I'd change in V3:

**Tenancy and geography should no longer look like the same thing.**

We've already refined the model so that a NATS account represents a hard isolation boundary, while an Operator/Region is merely the natural default boundary; they are not inherently synonymous.

Similarly, regional topology and tenant isolation are separate concerns: clusters/gateways deal with geography and HA, while accounts/permissions deal with isolation.

That distinction should become visually obvious in the diagrams.

---

## Where I think we need more expansion

Not necessarily **on V2/V3 themselves**. Instead, boxes on V2/V3 should lead to focused diagrams.

I would expect the eventual diagram tree to look something like:

```text
                     V2 SYSTEM OVERVIEW
                             │
                     V3 LOGICAL ARCHITECTURE
                             │
       ┌─────────────────────┼──────────────────────┐
       │                     │                      │
   BUSINESS             FUNCTIONAL             PLATFORM
   CONTEXT              ARCHITECTURE           ARCHITECTURE
       │                     │                      │
 Participants            Marketplace              NATS
 Organisations           Orders                   Temporal
 Operators               Shipping                 Data
 Users                   Fleet                    Identity
 Roles                   Tracking                 Observability
       │                 Documents
       │                 Billing
       │
       ├───────────────┐
       │               │
   TENANCY          SECURITY
       │               │
   Membership        RBAC
   Isolation         JWT
   Data scope        Permissions

                             │
                    NON-FUNCTIONAL VIEWS
                             │
               ┌─────────────┼──────────────┐
               │             │              │
          Multi-region      HA/DR       Deployment
               │             │              │
          Regions          Failure        Containers
          Gateways         Recovery       Compute
          Residency        Backup         Networking
```

And then some functional areas need another level again.

For example:

```text
V3
 │
 └── Shipping
       │
       ├── Order Architecture
       ├── Load Architecture
       ├── Trip Architecture
       ├── Route / Stop Architecture
       └── POD Workflow
```

or:

```text
V3
 │
 └── NATS
       │
       ├── Account / Tenant Model
       ├── Multi-Region Topology
       ├── Subject Architecture
       ├── JetStream Architecture
       ├── Exports / Imports
       └── Service Discovery / RPC
```

This is where all the useful detail we've already accumulated can live **without making V3 unreadable**.

### One addition I'd make to both diagrams

Use small references on important boxes:

```text
┌─────────────────────────┐
│          NATS           │
│                         │
│ See: A-06 Messaging     │
└─────────────────────────┘
```

```text
┌─────────────────────────┐
│     Organisations       │
│                         │
│ See: A-02 Tenancy       │
└─────────────────────────┘
```

```text
┌─────────────────────────┐
│       Temporal          │
│                         │
│ See: A-08 Workflows     │
└─────────────────────────┘
```

The overview then becomes an **index into the architecture**, not merely a picture.

### So does our previous work need changing?

Mostly **restructuring rather than redesigning**.

The things we've already worked out — regional NATS clusters, accounts, JetStream, Temporal, PostgreSQL, services, organisations, operators, MFE shell, observability, etc. — remain valid building blocks.

What's missing is the **architecture narrative around them**:

**Who uses the platform → what business capabilities it provides → what applications expose those capabilities → what services implement them → what platform technologies support them → where they run → how tenancy/security/resilience work.**

That's the sequence I would now make V2/V3 communicate.

And I think this actually gives us a good criterion for the next revision: **don't add more boxes to V2/V3 just because something is important. Add enough to establish the concept, then create a derived diagram where that concept can be explained properly.**

---

## User

Lets start with another diagram generation round using your recommendation

```

How I'd evolve the current structure

I would make the two diagrams serve different viewpoints rather than simply making V3 a more crowded V2.

Diagram	Purpose	What it should primarily show
V2 – System / Platform Overview	Explain the entire system to someone new	Actors, operators/markets, organisations, applications, major logistics capabilities, platform, regions
V3 – Logical / Technical Architecture	Explain how the platform is constructed	Shell/MFE, domain services, NATS, Temporal, data stores, platform services, regional boundaries
Derived diagrams	Explain one aspect properly	Tenancy, NATS topology, workflows, security, deployment, data, MFE, observability, etc.
```



Let's rename the architecture title for the next round to "Proposed Linebooker V3 Architecture" and let's remap:

- V2 to L1 (for level 1)
- V3 to L2 (for level  2)
- etc



You're thoughts?

## Assistant

Yes — I think this is a **much cleaner model**. The important improvement is that **“V3” now means the version of the proposed Linebooker architecture**, while **L1/L2/L3 describe the level of architectural abstraction**. Those are two different concepts and shouldn't be mixed.

So:

> **Proposed Linebooker V3 Architecture** = the architecture baseline/version  
> **L1, L2, L3…** = progressively deeper views of that architecture

That removes the current ambiguity where “V2” and “V3” could sound like competing architecture revisions rather than different levels of detail.

### I would structure it like this

| Level | Name | Primary question |
|---|---|---|
| **L1** | **System / Platform Overview** | What is the Linebooker platform and who/what participates in it? |
| **L2** | **Logical / Technical Architecture** | How is the platform logically constructed? |
| **L3** | **Domain / Concern Architecture** | How does one major area work? |
| **L4** | **Detailed Design / Implementation** | How is that area actually implemented/deployed? |

So the hierarchy becomes:

```text
             Proposed Linebooker V3 Architecture
                           │
                           ▼
                L1 – System / Platform Overview
                           │
                           ▼
                L2 – Logical / Technical Architecture
                           │
          ┌────────────────┼─────────────────┐
          │                │                 │
          ▼                ▼                 ▼
   L3 – Tenancy      L3 – Messaging    L3 – Workflows
   Architecture       Architecture      Architecture
          │                │                 │
          ▼                ▼                 ▼
   L4 – RBAC /       L4 – Subjects /   L4 – Temporal
   Membership        Streams / JWTs     Workflow Design
```

## L1

I'd make this the **executive/system context view**.

It should contain:

```text
Actors
  │
  ├── Platform Admins
  ├── Operator Users
  ├── Customer Users
  ├── Transporter Users
  └── Drivers

Operators / Markets
  │
Participating Organisations
  │
Applications
  │
Major Logistics Capabilities
  │
Platform
  │
Regions
```

But very little implementation detail.

Something roughly like:

```text
             PROPOSED LINEBOOKER V3 ARCHITECTURE
                      L1 – SYSTEM OVERVIEW


       USERS / PARTICIPANTS
                │
     ┌──────────┼───────────┐
     │          │           │
 Platform   Customer    Transporter
  Admins     Orgs          Orgs
                            │
                          Drivers
                │
                ▼
       OPERATORS / MARKETS
        ZA      AU      ...
                │
                ▼
         APPLICATIONS
                │
      Web / Mobile / APIs
                │
                ▼
      LOGISTICS CAPABILITIES
                │
 ┌────────┬────────┬────────┬────────┐
 │        │        │        │        │
Market  Orders   Fleet   Tracking  Billing
place   /Trips
                │
                ▼
      APPLICATION PLATFORM
                │
     NATS / Temporal / Data
                │
                ▼
        REGIONAL PLATFORM
```

The goal is that a business stakeholder can understand it without knowing what JetStream or Temporal is.

---

# L2

L2 should answer:

> **How is that system implemented conceptually?**

This is where most of the valuable technical content from the old V3 should move.

For example:

```text
           L2 – LOGICAL / TECHNICAL ARCHITECTURE


                   Application Clients
                          │
                          ▼
               ┌────────────────────┐
               │ Application Shell  │
               │       / MFE        │
               └─────────┬──────────┘
                         │
       ┌─────────────────┼──────────────────┐
       │                 │                  │
  Marketplace          Orders             Fleet
       │                 │                  │
       └─────────────────┼──────────────────┘
                         │
                  Domain Services
                         │
       ┌─────────────────┼───────────────────┐
       │                 │                   │
      NATS            Temporal             Data
       │                 │                   │
 Messaging/Event      Workflows          PostgreSQL
 RPC/JetStream        Activities         Object Store
                                           KV
                         │
                         ▼
                 Platform Services
                         │
             Identity / Observability
                         │
               ┌─────────┴─────────┐
               │                   │
            ZA Region           AU Region
```

At L2 we can also start representing:

- control plane vs regional plane
- service boundaries
- MFE architecture
- Identity
- observability
- NATS
- Temporal
- PostgreSQL
- object storage
- regional deployment boundaries

But **not** yet detailed NATS subjects, stream names, DB schemas, workflow states, Docker containers, etc.

---

# L3 is where this becomes particularly useful

Instead of having one giant L3, I'd have **multiple L3 views**.

For example:

```text
L3.1  Participant & Tenancy Architecture

L3.2  Functional Domain Architecture

L3.3  Application / MFE Architecture

L3.4  NATS Messaging Architecture

L3.5  Temporal Workflow Architecture

L3.6  Data Architecture

L3.7  Multi-Region Architecture

L3.8  Security & Identity Architecture

L3.9  Observability Architecture

L3.10 Integration Architecture
```

This is much more scalable than trying to put all of these concepts onto L2.

---

# L4 then becomes optional

Not every L3 needs an L4.

For example:

```text
L3.4 NATS Messaging Architecture
        │
        ├── L4.4.1 NATS Account Model
        ├── L4.4.2 Subject Taxonomy
        ├── L4.4.3 JetStream Streams
        ├── L4.4.4 Export / Import Model
        └── L4.4.5 Multi-Region Topology
```

Similarly:

```text
L3.5 Temporal Workflow Architecture
        │
        ├── L4.5.1 Transporter Onboarding
        ├── L4.5.2 Load Tender Workflow
        ├── L4.5.3 Trip Execution Workflow
        ├── L4.5.4 POD Workflow
        └── L4.5.5 Settlement Workflow
```

And:

```text
L3.1 Participant & Tenancy
        │
        ├── L4.1.1 Organisation Model
        ├── L4.1.2 Membership Model
        ├── L4.1.3 RBAC
        └── L4.1.4 Tenant Data Isolation
```

That gives the documentation room to grow naturally.

## I'd also introduce diagram IDs

Rather than referring to things casually as "the NATS diagram", I would use something like:

```text
LB-V3-L1-01   System / Platform Overview

LB-V3-L2-01   Logical / Technical Architecture

LB-V3-L3-01   Participant & Tenancy
LB-V3-L3-02   Functional Domains
LB-V3-L3-03   Application / MFE
LB-V3-L3-04   Messaging / NATS
LB-V3-L3-05   Workflow / Temporal
LB-V3-L3-06   Data
LB-V3-L3-07   Multi-Region
LB-V3-L3-08   Security
LB-V3-L3-09   Observability
```

Then underneath:

```text
LB-V3-L4-04.1   NATS Account Model
LB-V3-L4-04.2   NATS Subject Model
LB-V3-L4-04.3   JetStream Model
```

That may seem slightly formal now, but once there are 15–30 diagrams it becomes extremely useful.

---

## One small naming change I'd recommend

Instead of putting just:

**L1 – Overview**

I'd use:

> **Proposed Linebooker V3 Architecture**  
> **L1 — System & Platform Overview**

and:

> **Proposed Linebooker V3 Architecture**  
> **L2 — Logical & Technical Architecture**

That way screenshots make sense **even when viewed outside the architecture document**.

---

## Most importantly: L1 → L2 should feel like zooming in

I'd make this a design rule.

If L1 contains:

```text
┌────────────────────────┐
│ Application Platform   │
│                        │
│ NATS / Temporal / Data │
└────────────────────────┘
```

then L2 expands that concept:

```text
Application Platform
        │
 ┌──────┼───────────────┐
 │      │               │
NATS Temporal       Data Platform
 │      │               │
 │   Workflows       PostgreSQL
 │   Activities      Object Store
 │
Events / RPC
JetStream
```

And L3 expands one of those again.

So conceptually:

```text
L1
┌──────────────────────┐
│ Application Platform │
└──────────┬───────────┘
           │ zoom
           ▼

L2
┌──────┬──────────┬──────────┐
│ NATS │ Temporal │   Data   │
└──┬───┴──────────┴──────────┘
   │ zoom
   ▼

L3
┌───────────────────────────────┐
│ NATS Messaging Architecture  │
│                               │
│ Accounts / Regions / JS / RPC│
└───────────────┬───────────────┘
                │ zoom
                ▼

L4
Subjects / Streams / Consumers /
Exports / JWT permissions / etc.
```

That gives the whole architecture set a **consistent visual language**.

So yes: I'd proceed with **“Proposed Linebooker V3 Architecture” as the architecture version**, retire the old V2/V3 diagram terminology, and start the next diagram-generation round with **L1 System & Platform Overview** and **L2 Logical & Technical Architecture** as the two primary diagrams. Then we derive L3s rather than continuing to make L2 denser.

---

## User

Before we start, would you consider external integrations also as participants, like fleet tracking systems, WorkOS, ERP systems for financing, etc.

We can retire the old V2/V3 diagram terminology and we'll need to document the architectural structure too.

## Assistant

Yes — but I would make an important distinction.

I would **show external integrations on the L1 diagram**, because they are part of the system context, but I would **not classify all of them as “participants” in the same sense as Customers, Transporters, Operators and Users**.

A cleaner model is:

```text
                    LINEBOOKER ECOSYSTEM

 BUSINESS PARTICIPANTS                         EXTERNAL SYSTEMS
 ─────────────────────                         ────────────────

 Platform Admins                               Fleet Tracking / Telematics
 Operator Users                                ERP / Finance Systems
 Customer Organisations                       Identity Provider / WorkOS
 Transporter Organisations                    Maps / Geocoding
 Brokers / Agents                              Payment Providers
 Drivers                                       SMS / Email / Messaging
                                               Regulatory / Compliance APIs

                \                               /
                 \                             /
                  └────────────┬──────────────┘
                               ▼
                  ┌─────────────────────────┐
                  │       LINEBOOKER        │
                  │        PLATFORM         │
                  └─────────────────────────┘
```

That distinction will make the architecture much clearer.

## Business participants vs external systems

I would use **Participant** primarily for a person or organisation that participates in the logistics ecosystem.

For example:

| Type | Examples | Relationship to Linebooker |
|---|---|---|
| **Business Participant** | Customer, Transporter, Broker, Operator | Conducts business through the platform |
| **Human Actor** | Admin, Dispatcher, Planner, Driver, Finance user | Uses the platform |
| **External Operational System** | Fleet tracker, telematics provider, GPS platform | Exchanges operational data |
| **External Enterprise System** | SAP, Sage, Xero, ERP/accounting systems | Exchanges orders, invoices, settlements, master data |
| **Platform Dependency** | WorkOS | Provides a technical capability such as identity/authentication |
| **External Utility** | Maps, geocoding, SMS, email | Provides supporting capabilities |

So I wouldn't show:

```text
Participants
├── Customer
├── Transporter
├── Driver
├── WorkOS
└── SAP
```

because those things don't have equivalent relationships to the platform.

Instead:

```text
                     PLATFORM ECOSYSTEM
                             │
             ┌───────────────┴────────────────┐
             │                                │
     Business Participants             External Systems
             │                                │
    ┌────────┼─────────┐            ┌─────────┼─────────┐
    │        │         │            │         │         │
Customer Transporter Operator     Identity   ERP    Telematics
```

That becomes a useful architectural concept in itself.

---

# WorkOS is slightly different again

I would probably classify something such as **WorkOS** as an **external platform dependency**, rather than an integration partner.

For example:

```text
                     LINEBOOKER
                         │
        ┌────────────────┼─────────────────┐
        │                │                 │
      WorkOS           Maps             Messaging
     Identity         Provider            Provider
        │
        ▼
 Authentication
 SSO / Directory Sync
 User provisioning
```

Whereas an external ERP is more directly an **enterprise integration**:

```text
Customer ERP
     │
     │ Orders / Customers / Invoices
     ▼
Integration Boundary
     │
     ▼
Linebooker
```

And fleet tracking is more like an **operational integration**:

```text
Telematics / Fleet Tracking
             │
       GPS / Telemetry
             │
             ▼
       Tracking Service
             │
            NATS
             │
     Trips / ETA / Alerts
```

That's an important architectural distinction.

---

# This should change L1 slightly

I think the new **L1 — System & Platform Overview** should have three things across the top rather than just users.

Something along these lines:

```text
          PROPOSED LINEBOOKER V3 ARCHITECTURE
               L1 — SYSTEM & PLATFORM OVERVIEW


 ┌──────────────────┐                     ┌────────────────────┐
 │ HUMAN ACTORS     │                     │ EXTERNAL SYSTEMS   │
 │                  │                     │                    │
 │ Platform Admin   │                     │ Fleet Tracking     │
 │ Operator Users   │                     │ ERP / Finance      │
 │ Customer Users   │                     │ Identity / WorkOS  │
 │ Transport Users  │                     │ Maps               │
 │ Drivers          │                     │ Payments           │
 └────────┬─────────┘                     └─────────┬──────────┘
          │                                         │
          │       ┌───────────────────────┐         │
          └──────►│ BUSINESS PARTICIPANTS │◄────────┘
                  │                       │
                  │ Operators / Markets   │
                  │ Customer Orgs         │
                  │ Transporter Orgs      │
                  │ Brokers / Agents      │
                  └───────────┬───────────┘
                              │
                              ▼
                 ┌────────────────────────┐
                 │  LINEBOOKER PLATFORM   │
                 │                        │
                 │ Applications           │
                 │ Logistics Capabilities │
                 │ Platform Capabilities  │
                 └────────────┬───────────┘
                              │
                              ▼
                     Regional Platform
```

I'd keep the actual integrations fairly generic at L1.

So probably:

**ERP / Finance Systems**

rather than:

**SAP S/4HANA → REST → Finance Service → NATS → Settlement projection**

That belongs much lower down.

---

# L2 can introduce the integration boundary

This is where I think the revised architecture gets stronger.

Instead of external systems connecting directly into arbitrary domain services, L2 can establish an architectural boundary:

```text
                    EXTERNAL SYSTEMS
                           │
           ┌───────────────┼───────────────┐
           │               │               │
       Telematics         ERP           Identity
           │               │               │
           └───────────────┼───────────────┘
                           ▼
                ┌─────────────────────┐
                │ INTEGRATION LAYER   │
                │                     │
                │ APIs                │
                │ Webhooks            │
                │ Connectors          │
                │ Adapters            │
                │ Event ingestion     │
                └──────────┬──────────┘
                           │
                           ▼
                    Domain Services
                           │
                      NATS / Temporal
```

Whether there is literally an "Integration Service" is an implementation decision.

Architecturally, though, establishing an **integration boundary** is valuable.

---

# And then give integrations their own L3

I'd add this to the proposed architecture catalogue:

```text
L3 — External Integration Architecture
```

Then potentially derive:

```text
L3 Integration Architecture
        │
        ├── L4 Fleet / Telematics Integration
        │
        ├── L4 ERP / Finance Integration
        │
        ├── L4 Identity / WorkOS Integration
        │
        ├── L4 Maps / Geospatial Integration
        │
        ├── L4 Payment Integration
        │
        └── L4 Notification Integration
```

This is exactly what the L1 → L2 → L3 approach is intended to solve.

L1 merely says:

```text
External Systems
```

L2 says:

```text
External Systems
       ↓
Integration Boundary
       ↓
Domain Platform
```

L3 says:

```text
How integrations work architecturally
```

and L4 can finally explain something like:

```text
Fleet Tracking Provider
        │
        │ webhook / API / stream
        ▼
Telemetry Adapter
        │
        ▼
tracking.location.received
        │
       NATS
        │
        ├── Tracking
        ├── ETA
        ├── Geofence
        └── Trip Workflow
```

That hierarchy feels right.

---

# Documenting the architecture structure

I also agree that we should document the **architecture model itself before generating lots of diagrams**.

I'd make a small architecture guide part of **Proposed Linebooker V3 Architecture**.

It would define one hierarchy:

```text
PROPOSED LINEBOOKER V3 ARCHITECTURE
│
├── L1 — System & Platform Overview
│
├── L2 — Logical & Technical Architecture
│
├── L3 — Architectural Views
│   │
│   ├── Participant & Tenancy
│   ├── Functional Domains
│   ├── Application / MFE
│   ├── Integration
│   ├── Messaging / NATS
│   ├── Workflow / Temporal
│   ├── Data
│   ├── Multi-Region
│   ├── Security
│   └── Observability
│
└── L4 — Detailed Designs
    │
    ├── Subjects / Streams
    ├── Workflows
    ├── Schemas
    ├── Deployment
    ├── APIs
    ├── Integration adapters
    └── etc.
```

And I'd explicitly document another distinction that is becoming important:

```text
BUSINESS STRUCTURE
    Participants / Organisations / Users

FUNCTIONAL STRUCTURE
    Marketplace / Orders / Fleet / Tracking / Billing

APPLICATION STRUCTURE
    Web / Mobile / Shell / MFE

INTEGRATION STRUCTURE
    ERP / Tracking / Identity / External APIs

PLATFORM STRUCTURE
    NATS / Temporal / Data / Identity / Observability

DEPLOYMENT STRUCTURE
    Regions / Compute / Networking / Storage

NON-FUNCTIONAL STRUCTURE
    Security / HA / DR / Scaling / Residency
```

That gives us a **formal grammar for the diagrams** before we generate them.

And yes, I'd now treat the old V2/V3 naming as retired. From here on, I think the clean starting point is:

**Proposed Linebooker V3 Architecture — L1: System & Platform Overview**

followed by:

**Proposed Linebooker V3 Architecture — L2: Logical & Technical Architecture**.

Before generating the actual images, I'd recommend we define the **Architecture Structure / Diagram Catalogue** first—it can probably fit on one page and will act as the specification for every diagram we generate afterward.
