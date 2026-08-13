# Logistics Roles, NATS, and Linebooker-Style Marketplace Architecture

## Conversation

### User

How would you model the following logistics roles: - Admin (full root
access) - Operator (this could be a regional tenant) - customers -
Transporters

### Assistant

Given everything we've discussed around your logistics platform
(multi-region, NATS accounts, operators, customers, transporters, and
SaaS multi-tenancy), I'd separate **platform ownership** from **business
ownership**.

I would model it like this:

``` text
Platform
│
├── Admin (Platform Owner)
│
├── Region / Operator (Tenant)
│   │
│   ├── Customer
│   │
│   ├── Transporter
│   │
│   └── Internal Users
│       ├── Operator Admin
│       ├── Dispatcher
│       ├── Planner
│       ├── Finance
│       └── Support
│
└── Region / Operator (Tenant)
    ├── ...
```

## 1. Platform Admin

This is **not** a tenant.

Examples: - Your company - SaaS administrators - Support engineers

Responsibilities: - Create Operators - Manage subscriptions - Configure
regions - Global dictionaries - System monitoring - Billing -
Cross-tenant support

Think of this as the AWS root account.

## 2. Operator (Tenant)

I wouldn't call these simply "regions".

An **Operator** is the legal/business entity operating the logistics
platform within a territory.

Examples:

``` text
Linebooker South Africa
Linebooker Europe
Linebooker Australia

or

DHL South Africa
DP World Logistics
Kuehne + Nagel
```

An Operator owns: - customers - transporters - vehicles - users -
pricing - routes - loads - contracts

In your architecture this maps almost perfectly to a **NATS Account**.

``` text
Tenant (Operator)
    id
    name
    region
```

## 3. Customer

A customer requests transport.

Examples: - Toyota - Shoprite - Woolworths - Maersk

They create: - loads - shipments - purchase orders - delivery requests

A customer generally **does not own transport assets**.

``` text
Customer
    id
    tenantId
    company
```

## 4. Transporter

The company actually moving freight.

Examples: - Imperial Logistics - Barloworld - XYZ Trucking

They own: - trucks - trailers - drivers - depots

``` text
Transporter
    id
    tenantId
    company
```

A transporter may service many customers within the same Operator.

## Relationships

``` text
Platform
    │
    ├── Operator A
    │      │
    │      ├── Customer A
    │      ├── Customer B
    │      ├── Transporter X
    │      ├── Transporter Y
    │      └── Users
    │
    └── Operator B
           │
           ├── Customer C
           ├── Transporter Z
           └── Users
```

## Users belong to Organisations

Rather than assigning roles directly, users belong to an organisation.

``` text
User
    id
    organisationId
```

Organisation can be:

``` text
Operator
Customer
Transporter
```

Then roles become:

``` text
Platform Admin

Operator
    Admin
    Dispatcher
    Planner
    Finance
    Support

Customer
    Admin
    Operations
    Finance
    Tracking

Transporter
    Admin
    Dispatcher
    Driver Manager
    Fleet Manager
    Driver
```

This is much more extensible than hard-coding roles.

## NATS Mapping

I would align it like this:

``` text
Platform
    (_platform account)

Operator A
    (NATS Account)

Operator B
    (NATS Account)
```

Inside an Operator account:

``` text
customer.shoprite.*
customer.toyota.*

transporter.imperial.*
transporter.barloworld.*

load.*
trip.*
vehicle.*
driver.*
```

No separate NATS account per customer or transporter unless they require
complete isolation or operate independently.

## Database Model

``` text
Platform
    │
    └── Operator (Tenant)
            │
            ├── Organisation
            │       │
            │       ├── Customer
            │       ├── Transporter
            │       └── Operator
            │
            ├── User
            ├── Role
            ├── Load
            ├── Trip
            ├── Vehicle
            └── Driver
```

Notice that **Customer**, **Transporter**, and the **Operator itself**
are all specializations of a generic `Organisation` entity. That gives
you a single place for common fields (name, addresses, contacts, tax
IDs, etc.) while allowing organisation-specific data where needed.

## Recommendation

For the type of platform you're building, I'd avoid treating
**Customer** and **Transporter** as peer tenants. Instead:

-   **Platform** → owns the SaaS.
-   **Operator (Tenant)** → owns a region or operating business (maps to
    a NATS account).
-   **Organisation** → represents any business entity within that
    tenant.
    -   **Operator** (the tenant's own organisation)
    -   **Customer**
    -   **Transporter**

This hierarchy scales well because it naturally supports future
organisation types such as warehouses, customs brokers, shipping lines,
depots, agents, ports, carriers, or subcontractors without changing the
overall architecture.

------------------------------------------------------------------------

### User

How does this change when using NATs and replicating Linebooker

### Assistant

For a **Linebooker-like marketplace**, I'd change one important
assumption from my previous model: the **Operator is the platform
business itself**, while Customers and Transporters are first-class
organisations participating in that marketplace. The NATS account
boundary should follow **security/isolation**, not simply mirror every
business entity.

If you are specifically trying to replicate a **Linebooker-style freight
marketplace**, I would adjust the model. Linebooker sits between
shippers/customers and a network of approved transporters; its public
description emphasizes customers gaining access to selected transporters
and committed truck supply.

The business model I would use is:

``` text
                         PLATFORM
                      Logistics SaaS
                            │
               ┌────────────┴────────────┐
               │                         │
         South Africa                Australia
          Operator                    Operator
       / Marketplace               / Marketplace
               │                         │
        ┌──────┴──────┐
        │             │
    Customers     Transporters
        │             │
      Loads        Vehicles
      Orders       Drivers
      Tenders      Capacity
        │             │
        └──────┬──────┘
               │
          Loads / Trips
```

The key distinction is that **Customer and Transporter aren't children
of the Operator in the ownership sense**. They are independent
organisations participating in the Operator's marketplace.

### Separate the business model from the NATS security model

Your application model could therefore be:

``` text
Platform
│
├── Operator
│   ├── South Africa
│   ├── Namibia
│   └── Australia
│
├── Organisation
│   ├── Customer
│   └── Transporter
│
└── OperatorMembership
    ├── organisation_id
    ├── operator_id
    ├── status
    └── capabilities
```

That `OperatorMembership` becomes important.

For example, Woolworths could participate in several markets:

``` text
Woolworths
    │
    ├── member-of → South Africa Operator
    └── member-of → Namibia Operator
```

and a large transporter could similarly operate across several regions.

That is preferable to:

``` text
South Africa
└── Woolworths

Namibia
└── Woolworths
```

because you're otherwise duplicating the organisation.

## Then map that onto NATS

Here I would **not make every Customer and Transporter a NATS account
initially**.

NATS accounts provide hard isolation: each account has its own subject
namespace, and communication between accounts only happens through
explicit imports/exports. JetStream is isolated per account as well.

I would start with:

``` text
NATS Operator
│
├── $SYS
│
├── PLATFORM
│
├── ZA
│
├── NA
│
└── AU
```

where:

``` text
PLATFORM
    Global platform services

ZA
    South African marketplace

NA
    Namibian marketplace

AU
    Australian marketplace
```

So:

> **NATS Account ≈ operational/regional marketplace boundary, not
> Customer or Transporter.**

This fits what you were previously describing as your regional tenant.

### ZA account example

Inside the `ZA` account:

``` text
load.created
load.updated
load.cancelled

tender.created
tender.offered
tender.accepted
tender.rejected

trip.created
trip.started
trip.completed

tracking.position.updated

invoice.created
pod.received
```

Notice what is deliberately missing:

``` text
za.customer.woolworths.load.created
za.transporter.imperial.trip.started
```

You don't need `za` in the subject because the **NATS account already
provides the ZA namespace**.

And I generally wouldn't put the organisation into every subject either.

Instead, organisation identity belongs in the event/message:

``` json
{
  "loadId": "L12345",
  "customerId": "ORG-1001",
  "operatorId": "ZA",
  "transporterId": "ORG-8142"
}
```

Your service knows which organisations are allowed to interact with that
load.

## Where NATS gets interesting

Because you want browsers/apps connecting directly to NATS over
WebSockets, the distinction becomes more important.

Suppose:

``` text
Customer: Woolworths
Transporter: Imperial
Operator: Linebooker ZA
```

All three connect into:

``` text
NATS account: ZA
```

but receive different NATS user JWT permissions.

Conceptually:

``` text
                 NATS ACCOUNT: ZA
                       │
        ┌──────────────┼───────────────┐
        │              │               │
   Linebooker      Woolworths       Imperial
     Users           Users            Users
        │              │               │
   broad access   customer scope   transporter scope
```

For example:

``` text
Woolworths user

PUB
    commands.load.create
    commands.tender.create

SUB
    customer.ORG-1001.>
```

while:

``` text
Imperial user

PUB
    commands.tender.respond
    commands.tracking.update

SUB
    transporter.ORG-8142.>
```

and the operator might have:

``` text
Linebooker Operations

PUB/SUB
    >
```

The exact permissions can be encoded into the NATS user credentials/JWT
rather than creating a new NATS account for each company.

## Private organisation subjects

For WebSocket clients, I would have a distinction between **domain
subjects** and **organisation-facing subjects**.

Internally:

``` text
load.created
tender.created
trip.created
tracking.updated
```

Externally:

``` text
org.<orgId>.load.*
org.<orgId>.tender.*
org.<orgId>.trip.*
org.<orgId>.notification.*
```

For example:

``` text
org.woolworths.load.L12345.updated

org.imperial.tender.T7832.offered
```

Then the Woolworths JWT gets:

``` text
SUB org.woolworths.>
```

and cannot subscribe to:

``` text
org.picknpay.>
org.shoprite.>
```

This is a very natural place for NATS subject permissions.

## Customer ↔ Transporter communication

This is where the Linebooker marketplace model becomes particularly
useful.

I wouldn't normally allow:

``` text
Customer ──────────────────→ Transporter
```

direct messaging.

Instead:

``` text
Customer
   │
   │ create load
   ▼
Operator / Marketplace
   │
   │ matching/tender
   ▼
Transporter
```

For example:

``` text
Woolworths
     │
     │ command.load.create
     ▼
 Load Service
     │
     ├── load.created
     │
     ▼
 Tender Service
     │
     ├── selects eligible transporters
     │
     ▼
 org.imperial.tender.123.offered
```

That lets your platform enforce: - contracts - lanes - pricing -
transporter eligibility - vehicle requirements - capacity - blacklists -
compliance - tender rules

rather than NATS itself becoming an uncontrolled customer-to-transporter
bus.

## Where the PLATFORM account fits

I'd make the central account quite small.

``` text
PLATFORM
│
├── Identity/Auth
├── Tenant/Operator Registry
├── Organisation Registry
├── Global Reference Data
├── Platform Configuration
└── Platform Administration
```

Regional accounts then consume selected services/data.

``` text
                         PLATFORM
                             │
                   exports / imports
                ┌────────────┼────────────┐
                ▼            ▼            ▼
               ZA           NA           AU
```

For example:

``` text
PLATFORM exports

service.auth.>
service.organisation.>
reference.country.>
reference.currency.>
reference.vehicle-type.>
```

and:

``` text
ZA imports
NA imports
AU imports
```

You therefore don't need to duplicate global reference services into
every marketplace.

## The resulting model

I'd now describe your architecture as **four levels**, rather than the
hierarchy I gave previously:

``` text
1. PLATFORM
   └── Root administrative/security boundary

2. OPERATOR / MARKET
   └── Regional operational boundary
       e.g. ZA, NA, AU

3. ORGANISATION
   ├── Customer
   ├── Transporter
   └── potentially Agent/Broker/etc.

4. USER
   └── Member of an organisation
```

With the important relationship:

``` text
                 Platform
                    │
          ┌─────────┴─────────┐
          │                   │
      Operator ZA         Operator AU
          │                   │
     Marketplace          Marketplace
          │
 ┌────────┴─────────┐
 │                  │
Customer        Transporter
 │                  │
Users              Users
```

But in the **data model**, make membership many-to-many:

``` text
Organisation
      │
      │
      ▼
OperatorMembership
      │
      ▼
Operator
```

so eventually:

``` text
                   Woolworths
                       │
             ┌─────────┼─────────┐
             ▼         ▼         ▼
            ZA        NA        BW
```

without creating three Woolworths organisations.

## One thing I would change from our earlier NATS design

I previously leaned toward:

``` text
NATS Account = Tenant = Operator
```

I'd refine that to:

> **NATS Account = isolation boundary. Operator/Region is currently the
> natural isolation boundary, but it doesn't have to remain synonymous
> with tenant.**

That's a subtle but important architectural distinction.

Your model could eventually support:

``` text
Platform
   │
   ├── ZA                       ← NATS Account
   │    ├── 500 customers
   │    └── 300 transporters
   │
   ├── AU                       ← NATS Account
   │
   └── Enterprise-X             ← NATS Account
```

If a very large enterprise customer later requires hard messaging
isolation, you can give them their **own NATS account** and connect them
to the marketplace through NATS exports/imports.

So you retain a clean evolution path:

``` text
Normal customer
    ↓
Organisation inside ZA account

Large/regulated customer
    ↓
Dedicated NATS account
    ↓
imports/exports
    ↓
ZA marketplace
```

**Recommended Linebooker-like model:** Platform → Regional
Marketplace/Operator → participating Organisations → Users, while
letting **NATS accounts represent hard isolation boundaries rather than
blindly mirroring the domain hierarchy**.

------------------------------------------------------------------------

### User

Save this entire chat to markdown
