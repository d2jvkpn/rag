# Go Style Cheat Sheet

## 1. Variables

- Declare all variables at the top of a function with `var`; use them below — avoid `:=`
- Exception: temporary variables scoped to a loop (`for i, v := range ...`, `for i := 0; ...`) or to
  an `if` block may use `:=`
  ```go
  // good
  func process() error {
      var (
          result string
          err    error
      )
      result, err = fetch()
      for _, item := range result {  // loop variable — := ok
          ...
      }
      return err
  }

  // avoid
  func process() error {
      result, err := fetch()
      ...
  }
  ```

## 2. Configuration

- Prefer `viper.Get*` methods over declaring a struct and deserialising into it; only unmarshal when
  the config subtree is large enough that individual `Get` calls become unwieldy:
  ```go
  // good
  host := v.GetString("server.host")
  port := v.GetInt("server.port")

  // avoid unless the subtree has many fields
  var cfg ServerConfig
  v.UnmarshalKey("server", &cfg)
  ```

## 3. Unique IDs

- Always use **UUID v7** (`github.com/google/uuid`) for generating unique IDs — time-ordered,
  sortable, and suitable for database keys, session IDs, and event IDs:
  ```go
  id := uuid.Must(uuid.NewV7()).String()
  ```
- Do not use `uuid.New()` (v4) or `uuid.NewString()` (v4) for new code.

## 4. File Naming

- Use snake_case for multi-word filenames: `agent_builder.go`, `date_format.go`, `llm_provider.go`
- Single-word files need no separator: `service.go`, `session.go`
- Standard Go names are unchanged: `main.go`, `*_test.go`

## 5. Line Length

- Maximum **120 characters** per line.
- Break long function signatures and calls by placing arguments on indented lines with a trailing
  comma; closing `)` on its own line:
  ```go
  // good
  func NewFoo(
      ctx context.Context,
      cfg *Config,
      logger *zap.Logger,
  ) (*Foo, error) { ... }

  result, err = doSomething(
      arg1, arg2,
      arg3, arg4,
  )

  // avoid
  func NewFoo(ctx context.Context, cfg *Config, logger *zap.Logger) (*Foo, error) { ... }
  result, err = doSomething(arg1, arg2, arg3, arg4)
  ```
- Break long boolean conditions after `&&` / `||`:
  ```go
  if condA && condB &&
      condC && condD {
  ```
- Break long string literals with `+` concatenation:
  ```go
  msg := "first part of a very long string that " +
      "continues on the next line"
  ```
- Struct tags that exceed the limit use `//nolint:lll` (tags cannot span multiple lines).
- Comments (inline trailing `//` comments and standalone comment lines) are exempt from the limit
  — keep them on one line rather than wrapping.

## 6. General

- **Constructors** — `New*` prefix: `NewClient()`, `NewServer()`
- **Errors** — wrap with `fmt.Errorf("verb noun: %w", err)`; phrase the operation, not the failure:
  `"parse config: %w"` not `"failed to parse config: %w"`
  - `pkg/infra`'s structured error type (`Kind`/`Error`/`New`/`Wrap`/`ClassifyNetErr`) is for
    general-purpose, reusable packages whose errors may be inspected by a runtime caller (classified
    logging, `KindOf`, retry/fallback decisions) — e.g. `pkg/llm` (including
    `pkg/llm/openaicompat`), `pkg/memory/mem0`, `pkg/artifact/local`, `pkg/session/*`. This applies
    even if every current call site happens to run at startup (e.g. `pkg/llm/llm_provider.go`'s
    `LoadProviders`/`FindProvider`/`FindModel` are only called during agent-loader construction
    today): the package itself is reusable infrastructure, not one-off glue.
  - **Non-runtime errors** — one-off startup/wiring glue that isn't a reusable package, just
    assembles dependencies once and propagates up to `log.Fatalf` (e.g. `cmd/app.Build`,
    `cmd/adk`/`cmd/server`'s `main()`) — don't need `infra` wrapping; plain
    `fmt.Errorf("verb noun: %w", err)` is sufficient.
- **Comments** — inline only when the *why* is non-obvious; GoDoc for every exported symbol
- **Struct tags** — align across fields when a type has multiple tags
- **Tests** — same package (`package foo`); `t.Fatalf` for setup, `t.Errorf` for assertions
