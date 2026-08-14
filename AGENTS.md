# Agent & Contributor Guidelines (`AGENTS.md`)

This document outlines the core architecture principles, development workflows, quality gates, and best practices for developing and maintaining `qbit-gluetun-sync`.

All automated agents and human contributors must adhere to the rules in this document.

---

## 1. Non-Negotiable Pre-Commit Quality Gates

Before committing or submitting changes, all of the following steps **MUST** pass cleanly:

1. **Code Formatting (`gofmt`):**
   * Code must be formatted using the official Go formatter with simplification enabled:
     ```bash
     gofmt -s -w .
     ```
   * Ensure `git diff` shows zero formatting inconsistencies.

2. **Test Suite Must Pass:**
   * All package tests must pass with zero failures:
     ```bash
     go test -v -race ./...
     ```
   * The `-race` flag is mandatory to catch concurrency issues early.

3. **Static Analysis & Linting:**
   * Run `golangci-lint` to satisfy repository rules (including `errcheck`, `govet`, `staticcheck`, `gosec`, `unused`):
     ```bash
     golangci-lint run
     ```
   * Do not ignore lint warnings using `//nolint` unless accompanied by a justifiable reason in an inline comment.

4. **Dependency Hygiene:**
   * Ensure modules and sums are clean and tidy:
     ```bash
     go mod tidy
     git diff --exit-code go.mod go.sum
     ```

---

## 2. Golang Engineering Standards

### 2.1 Context & Lifecycle Management
* **Propagate Contexts:** Every function performing I/O, network requests, long-running loops, or background tasks must accept a `context.Context` parameter.
* **HTTP Requests:** Always use `http.NewRequestWithContext(ctx, ...)` instead of `http.NewRequest(...)`.
* **Graceful Termination:** Listen for `os.Interrupt` and `syscall.SIGTERM` using `signal.NotifyContext`. Ensure HTTP servers and goroutines shut down cleanly within a bounded timeout.

### 2.2 Error Handling & Logging
* **Wrap Errors for Context:** Wrap lower-level errors using `fmt.Errorf("failed to ...: %w", err)` to maintain traceable call stacks.
* **Never Swallow Errors Silently:** Either handle the error (with an appropriate fallback and log message) or return it up the stack.
* **Structured Logging:** Use `log/slog` via `pkg/logger`. Pass structured key-value pairs (e.g. `logger.Error("Failed to sync port", "port", port, "err", err)`).
* **Sanitize Log Output:** Never log sensitive credentials (passwords, auth tokens, session cookies) in plaintext.

### 2.3 Concurrency & State Safety
* **Mutex & State Protection:** Any shared state (e.g., `currentPort`, sync status) accessed across multiple goroutines must be synchronized using `sync.Mutex`, `sync.RWMutex`, or atomic types.
* **Goroutine Leaks:** Goroutines must have a deterministic exit condition tied to context cancellation or closed channels.

### 2.4 Testing Best Practices
* **Deterministic Tests:** Do not use arbitrary `time.Sleep(...)` calls to wait for asynchronous operations in tests. Use synchronization channels, `sync.WaitGroup`, or polling loops with timeout assertions.
* **Mock External Dependencies:** Use `httptest.Server` to mock external HTTP APIs (e.g. qBitTorrent WebUI, Gluetun API) and test both success and edge-case failure responses (e.g., `401`, `403`, `500`, partial payloads, timeouts).

---

## 3. Container & Docker Best Practices

Because this service is delivered as a containerized sidecar running in Docker Compose and Kubernetes environments, adhere to the following:

### 3.1 Minimal & Rootless Containers
* **Static Binary:** Build static Go binaries with CGO disabled:
  ```dockerfile
  CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o qbit-gluetun-sync ./cmd/sync
  ```
* **Distroless Base:** Use `gcr.io/distroless/static-debian12:nonroot` to minimize attack surface and eliminate unnecessary shared libraries.
* **Non-Root Execution:** Containers must run as an unprivileged non-root user (`USER nonroot:nonroot` or `UID 65532`).

### 3.2 Secret Management & Credentials
* **Support Secret Files (`_FILE` Convention):** Support both environment variables (e.g. `QBIT_PASS`) and file mounts (e.g. `QBIT_PASS_FILE`) to allow secure integration with Kubernetes Secrets and Docker Swarm secrets.
* **Precedence:** Secret files take precedence over plaintext environment variables.

### 3.3 HTTP Server Hardening
* **Configured Timeouts:** All `http.Server` instances must explicitly define defensive timeouts:
  * `ReadHeaderTimeout` (e.g. 5s)
  * `ReadTimeout` (e.g. 10s)
  * `WriteTimeout` (e.g. 30s)
  * `IdleTimeout` (e.g. 60s)
  * `MaxHeaderBytes` (e.g. 1MB)
* **Configurable Binding:** Support `LISTEN_ADDR` to allow restricting listener binding to `127.0.0.1` when operating within shared pod network namespaces.

### 3.4 Probing & Health Checks
* **Health Probes:** Provide dedicated endpoints:
  * `/healthz`: Liveness probe (process is running).
  * `/readyz`: Readiness probe (upstream is reachable and initial sync completed).
* **CLI Healthcheck:** Support a native `-healthcheck` flag on the binary so container orchestrators without `curl` (distroless) can run native container healthchecks.

---

## 4. Repository Structure & Conventions

```
.
├── cmd/
│   └── sync/               # Application entrypoint (main package, CLI routing, HTTP server)
├── pkg/
│   ├── logger/             # Structured logging wrappers (log/slog)
│   ├── qbit/               # qBitTorrent WebUI API client & preference manager
│   └── watcher/            # Inotify/fsnotify file & directory watcher engine
├── .golangci.yml           # Linter configuration
├── Dockerfile              # Multi-stage rootless distroless container build
└── AGENTS.md               # Repository rules & contributor guidelines
```

### Commit Guidelines
Follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:
* `feat:` A new feature or capability
* `fix:` A bug fix or stability correction
* `refactor:` Code restructuring without behavioral changes
* `test:` Adding or improving tests
* `docs:` Documentation updates
* `chore:` Maintenance, dependency bumps, tooling updates
