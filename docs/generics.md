# Type-Safe Generics

Use Go generics to resolve by type with compile-time safety. No type assertions needed.

## Options
- For[T](inj).Resolve()/MustResolve()
- ResolveByType[T](inj)/MustResolveByType[T](inj)

## Example: For[T] (fluent)

```go
inj := injector.NewInjector()
inj.Inject(NewDB)

// With error handling
if db, err := injector.For[*Database](inj).Resolve(); err == nil {
    _ = db
}

// Or panic on error
db := injector.For[*Database](inj).MustResolve()
```

## Example: ResolveByType (functions)

```go
inj := injector.NewInjector()
inj.Inject(NewDB)

db, err := injector.ResolveByType[*Database](inj)
if err != nil { /* handle */ }

// Or panic on error
mustDB := injector.MustResolveByType[*Database](inj)
_ = mustDB
```

## Notes
- Works best when all registrations are by type using Inject()
- Prefer Invoke for wiring multiple dependencies at once
