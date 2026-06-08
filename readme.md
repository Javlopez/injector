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
- **Lifecycle** — `Shutdown(ctx)` closes constructed `Shutdowner`s in reverse order
- **Groups** — collect many providers of one interface as a slice (`[]http.Handler`)
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

    // Fail fast: report every missing/ambiguous/cyclic dependency at startup,
    // before a single constructor runs.
    if err := inj.Validate(); err != nil {
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

## Lifecycle: Shutdown

Any constructed instance implementing `Shutdowner` is closed by `Shutdown`, in
reverse construction order. Pre-registered instances are left to their owner.

```go
type Shutdowner interface {
    Shutdown(ctx context.Context) error
}

defer inj.Shutdown(context.Background())
```

## Groups

Collect many providers of the same interface and resolve them as a slice — handy
for HTTP routes, middleware, plugins. Members are built fresh (not deduplicated
by type) while their dependencies come from the shared singleton registry:

```go
inj.InjectGroup("routes", NewUsersRoute)  // func(*Database) http.Handler
inj.InjectGroup("routes", NewOrdersRoute)

routes, err := injector.ResolveGroup[http.Handler](inj, "routes")
```

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
- [x] Thread-safety
- [x] Lifecycle management (`Shutdown` hooks)
- [x] Groups / multi-binding
- [x] Type-safe generic resolution (Go 1.18+)
- [ ] Scopes (singleton / transient / scoped)
- [ ] Configuration from files (JSON/YAML)

## FAQ

**Q: Is this thread-safe?**
A: Yes. Registration and resolution are guarded by a mutex; the factory call
itself runs without the lock so re-entrant factories don't deadlock. The suite
passes under `go test -race`. Concurrent first-resolution of the same type may
construct twice and keep one winner, so factories should be idempotent — in the
normal "build at startup" flow this never happens.

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
