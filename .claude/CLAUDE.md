# CLAUDE.md

## Commands

```bash
go run main.go          # run
go build .              # build
go test ./...           # test
go test -race ./...     # test with race detector
go test -run TestName   # single test
gofmt -w .              # format
go vet ./...            # vet
golangci-lint run       # lint
go mod tidy             # tidy modules
```

## Project

Go 1.26 module `github.com/owner/teetime`. CLI tool that finds golf tee times near a location by:
1. Geocoding the location via Nominatim (OpenStreetMap)
2. Finding nearby golf courses via Overpass API
3. Scraping each course's booking provider (ForeUP) for available tee times
4. Displaying results in a table

**Entry point:** `main.go` — parses flags (`--location`, `--date`, `--radius`, `--players`, `--holes`) and orchestrates the pipeline.

**Internal packages:**
- `internal/geo` — `Geocode` (Nominatim), `FindCourses` (Overpass API), `LatLng`/`Course` types
- `internal/scraper` — `ForeUpScheduleID`: scrapes a course website to extract its ForeUP schedule ID
- `internal/provider` — `Provider` interface + `TeeTime` type (shared contract for booking providers)
- `internal/provider/foreup` — ForeUP API client implementing `Provider`
- `internal/display` — `PrintTable`: renders `[]CourseResult` to stdout

No external Go dependencies (stdlib only).

---

## Go Guidelines

### Style
- Follow [Effective Go](https://go.dev/doc/effective_go).
- Always run `gofmt` before committing.
- Package names: lowercase, single word, no underscores.
- Multi-word names: `MixedCaps`. Acronyms all-caps: `HTTPServer`, `userID`.
- Keep functions short — if it doesn't fit on a screen, split it.

### Error Handling
- Never ignore errors. Discard with `_` only if justified with a comment.
- Wrap with context: `fmt.Errorf("doing X: %w", err)`.
- Inspect with `errors.Is` / `errors.As`, not string matching.
- Sentinel errors: `var ErrXxx = errors.New(...)` at package level.
- Return early on error — avoid deep nesting.

```go
// Good
result, err := doThing()
if err != nil {
    return fmt.Errorf("doThing: %w", err)
}

// Bad — 40 lines of logic inside if err == nil
```

### Naming
- Short, descriptive names. Single-letter (`i`, `v`, `k`) fine in tight scopes.
- Interfaces: `-er` suffix (`Reader`, `Stringer`).
- Constructors: `NewXxx(...)`.
- Receivers: 1-2 letter abbreviation, consistent across all methods on the type.
- No stutter: `http.Server` not `http.HTTPServer`; `user.ID` not `user.UserID`.

### Types & Interfaces
- Structs: small, single responsibility.
- Embed to compose, not to inherit.
- Value receivers unless mutation or copy cost requires pointer. Be consistent.
- `iota` enums: define a zero-value sentinel (`Unknown = iota`).
- Accept interfaces, return concrete types.
- Small interfaces — prefer single-method where possible.
- Define interfaces at the consumer, not in the implementing package.

### Concurrency
- Channels for signalling; mutexes for shared state.
- `context.Context` as first param for anything that blocks, does I/O, or can be cancelled.
- `sync.WaitGroup` or `errgroup.Group` to track goroutine lifecycles.
- Never start a goroutine without knowing how it will stop.

### Testing
- Table-driven tests with `t.Run`.
- Use `t.Helper()` in test helpers.
- Test behaviour, not implementation internals.
- Don't mix assertion libraries.
- Run with `-race` in CI.

### Packages & Dependencies
- One clear purpose per package. Avoid circular imports.
- Prefer stdlib over third-party for simple tasks.
- Run `go mod tidy` before committing.

### Performance
- Profile before optimising (`pprof`).
- `strings.Builder` over `+` in loops.
- `make([]T, 0, n)` when size is known ahead of time.

### Documentation & Security
- Every exported symbol needs a doc comment starting with its name.
- Comments explain *why*, not *what*.
- Never log secrets or credentials.
- Use `crypto/rand` for unpredictable values.
- Validate all external input at the boundary; use parameterised queries for SQL.

---

## Self-Correcting Rules Engine

**At session start, read the entire "Learned Rules" section before doing anything.**

Rules accumulate below as mistakes are made or preferences are stated. Each rule is numbered, never deleted. Higher-numbered rules win conflicts.

Format: `N. [CATEGORY] Never/Always do X — because Y.`
Categories: `[STYLE]` `[CODE]` `[ARCH]` `[TOOL]` `[PROCESS]` `[DATA]` `[UX]` `[OTHER]`

Add a rule when:
- The user corrects your output
- You made a wrong assumption about this codebase
- The user states a preference ("always use X", "never do Y")

---

## Learned Rules

<!-- New rules are appended below this line. Do not edit above this section. -->
