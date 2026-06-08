# Injector

[![Go Reference](https://pkg.go.dev/badge/github.com/Javlopez/injector.svg)](https://pkg.go.dev/github.com/Javlopez/injector)
[![Go Report Card](https://goreportcard.com/badge/github.com/Javlopez/injector)](https://goreportcard.com/report/github.com/Javlopez/injector)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A simple, lightweight dependency injection container for Go. Register constructors
in any order; the container wires the whole graph by type.

## Highlights

- **Recursive auto-wiring** — a factory's parameters are resolved from the
  registry automatically, so registration order does not matter
- **Interface binding** — a concrete registration satisfies an interface
  parameter; ambiguous matches are reported instead of guessed
- **Cycle detection** with the full path (`A → B → A`)
- **Contextual errors** — a missing dependency names the chain that required it
- **`Validate()`** — statically check the whole graph at startup *without
  constructing anything*, reporting every problem at once
- **`Build()`** — eagerly construct everything so runtime constructor failures
  (a panic, a failing `Ping`) surface at startup, not on the first request
- **Panic recovery** — a constructor panic is recovered and annotated with the path
- **Strict mode** — flag accidental duplicate registrations
- **Lifecycle** — `Start(ctx)` / `Shutdown(ctx)` via interfaces, with start rollback
- **Modules** — a module is a plain `func(*Injector)`, composed with `Apply` (no DSL)
- **Groups** — collect many providers of one interface as a slice (`[]http.Handler`)
- **Struct parameters** — `In`-embedded structs for constructors with many deps
- **Named instances** — several providers of one type (`primary`/`replica` DB)
- **Thread-safe** registration and resolution
- Type-safe generics: `For[T]`, `ResolveByType[T]`, `Get[T]`, `Must[T]`

## Installation

```bash
go get github.com/Javlopez/injector
```

## Quick Start

Register by type and resolve. A factory's parameters are auto-wired:

```go
type Database struct{ Name string }
func NewDB() *Database { return &Database{Name: "production-db"} }

type Repo struct{ DB *Database }
func NewRepo(db *Database) *Repo { return &Repo{DB: db} } // db is auto-wired

inj := injector.NewInjector()
inj.Inject(NewRepo) // order does not matter
inj.Inject(NewDB)

repo := injector.Must[*Repo](inj)
fmt.Println(repo.DB.Name) // production-db
```

## Composition root (end-to-end)

A realistic `repo → service → handler` wiring with shared singletons, an
interface boundary, startup validation and graceful shutdown:

```go
type UserRepo interface{ Find(id string) string }

type sqlUserRepo struct{ db *sql.DB }
func (r *sqlUserRepo) Find(string) string { return "alice" }
func NewUserRepo(db *sql.DB) UserRepo { return &sqlUserRepo{db: db} } // returns the interface

type UserService struct {
    repo   UserRepo
    logger *slog.Logger
}
func NewUserService(r UserRepo, l *slog.Logger) *UserService {
    return &UserService{repo: r, logger: l}
}

type UserHandler struct{ svc *UserService }
func NewUserHandler(s *UserService) *UserHandler { return &UserHandler{svc: s} }

func BuildContainer(db *sql.DB) (*injector.Injector, error) {
    inj := injector.NewInjector()

    // Shared leaf singletons.
    inj.Inject(db)
    inj.Inject(func() *slog.Logger { return slog.Default() })

    // Providers — any order.
    inj.Inject(NewUserHandler)
    inj.Inject(NewUserService)
    inj.Inject(NewUserRepo)

    // Fail fast at startup: Build validates the graph (every missing/ambiguous/
    // cyclic dependency) AND eagerly constructs everything, so a constructor
    // that only fails at runtime surfaces here instead of on the first request.
    if err := inj.Build(); err != nil {
        return nil, err
    }
    return inj, nil
}

func main() {
    inj, err := BuildContainer(openDB())
    if err != nil {
        log.Fatal(err)
    }
    defer inj.Shutdown(context.Background()) // closes Shutdowners in reverse order

    h := injector.Must[*UserHandler](inj)
    _ = h
}
```

## Auto-wiring and interface binding

`Inject(factory)` keys the factory by its first return type. When a parameter is
an **interface**, the container satisfies it from a concrete registration via
assignability:

```go
type Mailer interface{ Send(to string) error }
type ResendMailer struct{}
func (*ResendMailer) Send(string) error { return nil }
func NewResendMailer() *ResendMailer { return &ResendMailer{} }

inj.Inject(NewResendMailer)
m := injector.Must[Mailer](inj) // *ResendMailer satisfies Mailer
```

If two concrete types satisfy the same interface, the request is **ambiguous**
and returns an error listing the candidates rather than picking one. Pin it
explicitly with `Override` (see below).

A factory may return `(T, error)`; a non-nil error aborts resolution and is
wrapped (unwrappable with `errors.Is`).

## Struct parameters (many dependencies)

Constructors with a long positional parameter list become noisy and easy to
misorder. Embed `In` in a struct and each exported field is resolved by type:

```go
type ServiceDeps struct {
    injector.In
    DB       *sql.DB
    Logger   *slog.Logger
    Users    UserRepo
    Mailer   Mailer `optional:"true"` // left nil if not registered
}

func NewService(d ServiceDeps) *Service {
    return &Service{db: d.DB, logger: d.Logger, users: d.Users, mailer: d.Mailer}
}

inj.Inject(NewService) // d.* fields are field-wired from the container
```

A field tagged `optional:"true"` resolves to its zero value when unregistered,
instead of failing. `Validate` and `Build` expand the struct and check each
field individually.

## Validate (eager graph check)

`Validate()` walks every registered provider and group member **without calling
any constructor**, aggregating all problems:

```go
if err := inj.Validate(); err != nil {
    log.Fatalf("DI graph is broken:\n%v", err)
}
```

Sample output for a misconfigured graph:

```
injector: no dependency found for type *app.Database (required by *app.Handler → *app.Service → *app.Repo → *app.Database)
injector: cyclic dependency detected: *app.CycA → *app.CycB → *app.CycA
injector: ambiguous dependency for app.Mailer: 2 candidates assignable (*app.ResendMailer, *app.OtherMailer)
```

## Build (eager construction) & Strict mode

`Validate()` proves the graph is *resolvable* but constructs nothing. `Build()`
goes further: it validates, then eagerly builds every provider, so a constructor
that only fails at runtime is caught at startup.

```go
err := inj.Build()
// injector: factory for *app.Database failed: connection refused
```

A recovered constructor panic is annotated with the resolution path:

```
injector: panic constructing *app.Repo (*app.Handler → *app.Service → *app.Repo): runtime error: nil pointer dereference
```

`Strict()` turns accidental double-wiring into a reported error:

```go
inj := injector.NewInjector().Strict()
inj.Inject(NewDB)
inj.Inject(NewDB) // Validate/Build now report: duplicate registration for *app.Database
```

## Modules

A module is just a `func(*Injector)` — no `Provide`/`Option` DSL. Split a large
composition root into focused units and compose them with `Apply`:

```go
func wireData(i *injector.Injector)    { i.Inject(NewDB); i.Inject(NewUserRepo) }
func wireBilling(i *injector.Injector) { i.Inject(NewStripeClient); i.Inject(NewBillingService) }

inj := injector.NewInjector().Apply(wireData, wireBilling, wireAuth)
```

## Lifecycle: Start & Shutdown

Two symmetric interfaces, discovered automatically on constructed instances — a
constructor implements them, it never depends on the container (unlike
`fx.Lifecycle`):

```go
type Startable interface {
    Start(ctx context.Context) error // run in construction order
}
type Shutdowner interface {
    Shutdown(ctx context.Context) error // run in reverse construction order
}

if err := inj.Start(ctx); err != nil { // starts everything; rolls back on failure
    log.Fatal(err)
}
defer inj.Shutdown(context.Background())
```

`Start` runs every `Startable` in dependency order; if one fails it shuts down
the already-started components (in reverse) before returning. Pre-registered
instances are left to their owner.

## Groups

Collect many providers of the same interface and resolve them as a slice — handy
for HTTP routes, middleware, plugins. Members are built fresh (not deduplicated
by type) while their dependencies come from the shared singleton registry:

```go
inj.InjectGroup("routes", NewUsersRoute)  // func(*Database) http.Handler
inj.InjectGroup("routes", NewOrdersRoute)

routes, err := injector.ResolveGroup[http.Handler](inj, "routes")
```

## Named instances

Register several providers of the same type under a qualifier, then resolve by
name — or pull them into an `In`-struct with a `name:"..."` tag:

```go
inj.InjectQualified("primary", NewPrimaryDB) // func() *sql.DB
inj.InjectQualified("replica", NewReplicaDB) // func() *sql.DB

primary, _ := injector.GetNamed[*sql.DB](inj, "primary")

type RepoDeps struct {
    injector.In
    Writer *sql.DB `name:"primary"`
    Reader *sql.DB `name:"replica"`
}
func NewRepo(d RepoDeps) *Repo { ... }
```

Qualified factory parameters are auto-wired, results are cached (singleton per
name), and `Validate`/`Build` cover them like any other provider.

## Conditional registration & Override

Provider selection (real vs noop) stays in your code — a container wires what it
is given. These helpers keep that branch tidy:

```go
// Register primary when configured, fallback otherwise.
inj.InjectOr(os.Getenv("RESEND_API_KEY") != "", NewResendMailer, NewNoopMailer)

// Register only when a condition holds.
inj.InjectIf(featureEnabled, NewFeatureService)
```

`Override[T]` binds an explicit value to a type — including an interface — for
tests, or to resolve an ambiguity deterministically:

```go
Override[Mailer](inj, &mockMailer{}) // swap one collaborator in a test
```

## Name-based API

A separate, type-agnostic registry is available via `InjectByName` / `Resolve`
for cases where you key by string. Name-based factories are zero-argument (no
auto-wiring). Prefer the type-based API for new code.

## Testing

```bash
go test ./...
go test -race ./...   # registration and resolution are concurrency-safe
```

## Performance

Benchmarks on Linux amd64 (13th Gen Intel Core i9-13980HX):

- Resolve instance: ~5.8 ns/op, 0 B/op, 0 allocs/op
- MustResolve: ~5.6 ns/op, 0 B/op, 0 allocs/op
- Resolve from factory (cold): ~273 ns/op, 40 B/op, 2 allocs/op

Factory functions run once per type; subsequent resolves are cached and as fast
as instance resolution.

## Roadmap

- [x] Recursive auto-wiring by type
- [x] Interface binding with ambiguity detection
- [x] Circular dependency detection (with path)
- [x] Contextual resolution errors
- [x] Eager graph validation (`Validate`)
- [x] Eager construction (`Build`) with panic recovery
- [x] Strict mode (duplicate-registration detection)
- [x] Thread-safety
- [x] Lifecycle management (`Shutdown` hooks)
- [x] Groups / multi-binding
- [x] Type-safe generic resolution (Go 1.18+)
- [x] Struct-field parameters (`In`) for large constructors
- [x] Named / qualified instances (two `*sql.DB`, primary/replica)
- [x] Provider modules (`func(*Injector)` + `Apply`)
- [x] Ordered start hooks (`Start` with rollback)
- [x] Exactly-once construction (per-key `sync.Once`)
- [ ] Scopes (singleton / transient / scoped)

## FAQ

**Q: Is this thread-safe?**
A: Yes. Each provider is memoized behind a per-key `sync.Once`, so a factory
runs **exactly once** even under concurrent first-resolution — every caller
observes the same instance. The factory itself runs without the global lock held,
so re-entrant factories don't deadlock. The suite passes under `go test -race`.

**Q: How is registration order handled?**
A: It is irrelevant. Parameters are resolved recursively from the registry when
you resolve a type, so you can `Inject` constructors in any order.

**Q: What happens if a dependency is missing or cyclic?**
A: Resolution returns a `*ResolveError` naming the type and the chain that
required it. Run `Validate()` at startup to surface every such problem at once
before serving traffic.

**Q: Can I register multiple implementations of one interface?**
A: Yes, via groups (`InjectGroup` + `ResolveGroup[T]`). A single-value interface
request with more than one assignable concrete type is reported as ambiguous;
use `Override[T]` to pin one.

**Q: How does this compare to google/wire or uber-go/fx?**
A: `wire` generates code and keeps compile-time safety; this container resolves
at runtime via reflection (simpler, no codegen, but errors surface at startup
rather than at `go build`). `fx` is a heavier application framework with modules
and lifecycle. This library targets simplicity with the essential production
features (validation, lifecycle, groups, thread-safety) included.

## License

MIT — see [LICENSE](LICENSE).
```
