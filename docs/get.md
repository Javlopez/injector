# Get helper

Get is a shortcut for resolving by type and returning (T, error).

## API
- Get[T](inj) (T, error)

## Example

```go
inj := injector.NewInjector()
inj.Inject(NewDB)

if db, err := injector.Get[*Database](inj); err == nil {
    _ = db
} else {
    // handle not registered
}
```

## Notes
- Equivalent to ResolveByType[T](inj)
- Prefer the fluent For[T](inj).Resolve() when you want method-style chaining
