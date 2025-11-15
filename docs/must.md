# Must helpers

Must variants panic on error. They’re concise, but best used at startup or places where failing fast is acceptable.

## APIs
- MustResolveByType[T](inj) T
- Must[T](inj) T (shortcut)

## Examples

```go
inj := injector.NewInjector()
inj.Inject(NewDB)

// Using function-based API
db := injector.MustResolveByType[*Database](inj)

// Using shortcut
db2 := injector.Must[*Database](inj)
_ = db2
```

## When to use
- Application startup wiring
- Tests and examples for brevity

## When to avoid
- Request paths or long-running processes where panics are undesirable
