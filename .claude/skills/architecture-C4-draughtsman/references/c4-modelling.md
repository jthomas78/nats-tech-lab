# C4 Modelling Rules

The thinking half of the `architecture-C4-draughtsman` skill. Adapted from
the MIT-licensed `c4-architecture` skill in
[davila7/claude-code-templates](https://github.com/davila7/claude-code-templates/tree/main/cli-tool/components/skills/creative-design/c4-architecture),
with its Mermaid syntax and output conventions removed and this repository's
rules substituted. Original C4 model by Simon Brown.

## Levels

| Level | View | Audience | Shows | Draw it when |
|---|---|---|---|---|
| 1 | **Context** | Everyone | The one system, the people who use it, the external systems it touches | Always. It frames the scope for every other view |
| 2 | **Container** | Technical | Separately deployable or runnable things — applications, services, datastores, streams | Almost always |
| 3 | **Component** | Developers | The parts inside one container | Only when it answers something Container cannot |
| — | **Deployment** | Operations | Instances placed on infrastructure nodes | For a production or production-shaped system |
| — | **Dynamic** | Technical | One numbered sequence of interactions | Only for a flow the static views genuinely fail to explain |

**Context plus Container is enough for most work.** Component and Code views
are the ones most often drawn out of completeness rather than need. Start at
Context; stop as soon as the question is answered.

### The container test

A **container** is separately deployable or separately runnable. A service, a
single-page application, a database, a stream, a scheduled job.

A **component** is not separately deployable. It ships inside a container.

If it cannot be started, stopped, or deployed on its own, it is not a
container. This one distinction fixes most broken C4 diagrams.

## Mapping to the Proposed Linebooker V3 levels

The two schemes are close but not identical. Do not treat them as
interchangeable.

| C4 view | Nearest LB-V3 level | Note |
|---|---|---|
| — | L0 | The catalogue atlas has no C4 equivalent |
| Context | L1 | Close match |
| Container | L2 | Close match. LB-V3 L2 also carries selection and rationale card views, which C4 has no form for |
| Component | L3 | LB-V3 L3 also carries flow, state, trust and mechanism schematics |
| Deployment | L3 or L4 | Depends on whether it names instances or specifications |
| Dynamic | L3 schematic | — |
| — | L4 | LB-V3 L4 goes to subjects, schemas, endpoints and permission claims — below C4's code level |

## What every element must carry

1. **A name.** Specific. `Order Service`, not `Service 2`.
2. **A kind**, stated on the element, not implied by colour alone.
3. **A technology**, at Container level and below. `Go`, `PostgreSQL 16`,
   `NATS JetStream`, `Vue 3`.
4. **A responsibility**, in one line, under 50 characters where possible.

A missing type label is not a simplification. It is a removed fact.

## What every relationship must carry

1. **One direction.** Never a bidirectional arrow — it hides which side
   starts the interaction. Draw two lines if both directions are real.
2. **An action verb** that reads correctly in the direction drawn.
   `Publishes ship.arrived to`, `Reads container state from`.
   Not `Uses`, `Calls`, `Talks to`, `Interacts with`.
3. **A protocol**, at Container level and below. `HTTPS/JSON`,
   `NATS request/reply`, `JetStream`, `WebSocket`, `SQL`.
4. **The right two endpoints.** A line must connect the elements it actually
   describes. A label about one element must not terminate on its neighbour.

## Anti-patterns

Check the element list against these **before** drawing.

1. **Container/component confusion.** Apply the container test above.
2. **A shared library drawn as a container.** A library is not deployable.
   It is a component, or it is not on the diagram at all.
3. **One big message-broker box.** Do not draw a single `NATS` or `Kafka`
   rectangle. Draw the streams, subjects or topics that actually carry the
   flow — `SHIPPING` stream, `evt.*.shipping.ship.*` subject. A single broker
   box hides every routing decision, which is usually the interesting part.
4. **Invented levels.** No "subcomponents", no "sub-systems", no "modules"
   as a fifth tier. If it does not fit a level, it belongs at a different
   level.
5. **Internals of an external system.** An external system is one opaque box.
   You do not own it and its internals will change without telling you.
6. **A vague subsystem box.** `Backend`, `Platform`, `Core`. Name the real
   containers or leave the box off.
7. **Deployment detail in a Container view.** Regions, instance counts,
   availability zones and node types belong in a Deployment view.
8. **A Deployment view with no stated environment.** Say which one —
   production, staging, a single region, all regions.
9. **More than 20 elements.** Split into several views. Never shrink type
   labels or drop descriptions to fit.
10. **All levels drawn by default.** Draw what answers the question.
11. **Inconsistent notation across a set.** One element, one name, one
    colour, one kind, in every view of the set.
12. **The decision process on the diagram.** Options considered, rejected
    alternatives and rationale belong in an ADR, not in boxes and arrows.
    Point at the ADR from the sheet instead.

## Audience

| Audience | Views |
|---|---|
| Executives | Context |
| Product | Context, Container |
| Architects | Context, Container, key Components |
| Developers | Any, as needed |
| Operations | Container, Deployment |

## Patterns worth knowing

### Microservices

- **One team owns them all** — draw each service as a **container** inside
  one system boundary.
- **Separate teams own them** — promote each to its own **software system**
  in a Context view, and name the owning team in its description.

The deciding question is ownership, not count.

### Event-driven

Draw each stream, subject family or topic as its own queue-kind container.
Show which container publishes to it and which subscribes. Never route every
publisher and subscriber through one broker box.

In this repository, that means naming the actual stream (`SHIPPING`,
`REFDATA`) and the actual subject pattern, not the word `NATS`.

### CQRS and projections

Show the write path and the read path as separate labelled routes. Name the
role of each store explicitly — transactional truth, durable fact history,
derived read model, cache — so the diagram states which one a reader may
trust.

### Deployment

Instances are placed **inside** nesting nodes. An element floating beside a
node has no home, which is a defect, not a layout choice. Every deployed
element states where it runs.

## Review checklist

Score a finished C4 view against this list.

- [ ] The sheet names its system, its level and its scope in one line
- [ ] Every element states its kind
- [ ] Every element at Container level and below names its technology
- [ ] Every element has a one-line responsibility
- [ ] Every line is unidirectional
- [ ] Every line has an action-verb label reading correctly in its direction
- [ ] Every line at Container level and below names its protocol
- [ ] No bare `Uses` / `Calls` / `Talks to`
- [ ] 20 elements or fewer
- [ ] No single broker box standing in for real topics
- [ ] No external system's internals shown
- [ ] No deployment detail in a Container view
- [ ] A legend covers every colour, every line style and every acronym
- [ ] Names, colours and kinds match every sibling view in the set
- [ ] No C4 palette, no stick figures, no vendor logos
- [ ] The sheet is readable with no narrator present
