# Business rules — demo 04 odometer

Every rule below is enforced in `cqrs/domain.go` and proved by one Ginkgo
`Context` in `cqrs/domain_test.go`. Nothing is enforced in `main.go`,
`write.go` or `read.go`.

| ID | Rule | Error | Enforced by |
|---|---|---|---|
| BR-OD01 | `km` must be greater than 0 | `ErrNonPositiveKm` | `Vehicle.Travel` |
| BR-OD02 | A vehicle must be registered before it travels | `ErrNotRegistered` | `Vehicle.Travel` |
| BR-OD03 | A vehicle cannot be registered twice | `ErrAlreadyRegistered` | `Vehicle.Register` |
| BR-OD04 | A retired vehicle refuses trips | `ErrRetired` | `Vehicle.Travel` |
| BR-OD05 | A vehicle cannot be retired unless it is registered | `ErrNotRegistered` / `ErrRetired` | `Vehicle.Retire` |

## Notes that are easy to lose

**BR-OD01 is why the total can only rise.** Without it, one corrupt event
lowers a total on every future replay, and a total that can fall is a total
nobody trusts.

**BR-OD03 also covers a retired vehicle.** A retired vehicle has been
registered once already, and "twice" is what the rule forbids. Re-registering
one returns `ErrAlreadyRegistered`, not a fresh registration.

**BR-OD02 and BR-OD04 are checked before BR-OD01.** A caller who is told
"vehicle is retired" and a caller who is told "km must be greater than 0" need
different fixes, so the lifecycle answer wins.

**No rule reads a total.** That is why `Vehicle` (the aggregate) has no
`TotalKm` and `Odometer` (the read model) does. If a future rule needs a total
— a service interval, say — it moves into the aggregate then, and not before.
