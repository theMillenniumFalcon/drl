# drl — Distributed Rate Limiter

A production-ready, Redis-backed distributed rate limiter written in Go. Enforces request limits consistently across **multiple application instances** using atomic Lua scripts to guarantee correctness under concurrent load.

---

## Why Distributed?

A single-server application can enforce rate limits with a simple in-memory counter. Once you scale horizontally, that breaks:

```
User sends 100 requests/min limit

  ┌─────────────┐     ┌─────────────┐
  │  Server A   │     │  Server B   │
  │  counter=60 │     │  counter=60 │  ← 120 total, but each server
  └─────────────┘     └─────────────┘    thinks the user is under the limit
```

`drl` solves this by storing all counters in a single Redis instance (or Redis cluster) that every application server reads from and writes to atomically:

```
  ┌─────────────┐     ┌─────────────┐
  │  Server A   │     │  Server B   │
  └──────┬──────┘     └──────┬──────┘
         │                   │
         └─────────┬─────────┘
                   ▼
           ┌───────────────┐
           │     Redis     │  ← single source of truth
           │  counter=120  │     → 429 Too Many Requests
           └───────────────┘
```

---

## How It Works

`drl` implements a **fixed window counter** algorithm:

1. Every request increments a Redis key scoped to `(client_identifier, rule_name)`.
2. The key is created with a TTL equal to the window duration on first write.
3. Subsequent requests within the window increment the counter without resetting the TTL.
4. When the counter exceeds the limit, the server returns **429 Too Many Requests**.
5. After the TTL expires, Redis automatically deletes the key and the window resets.

The `INCR + EXPIRE` sequence is executed inside a **Lua script** that Redis runs atomically, so there are zero race conditions between concurrent goroutines or servers.

```
Request arrives
      │
      ▼
 Extract key  (IP / API key / user ID)
      │
      ▼
 Run Lua script in Redis  ──► INCR key
      │                        if new: EXPIRE key <window>
      │                        return {count, ttl, limit}
      ▼
 count ≤ limit?
   YES → set X-RateLimit-* headers → pass to handler
    NO → set Retry-After header    → return 429
```

---

## Project Structure

```
github.com/themillenniumfalcon/drl/
│
├── main.go                    # Entry point: wires config, Redis, router, graceful shutdown
│
├── config/
│   └── config.go              # Reads env vars; defines ServerConfig, RedisConfig, LimitConfig
│
├── limiter/
│   ├── limiter.go             # Limiter interface, Rule and Result types
│   ├── redis_limiter.go       # Redis-backed Limiter implementation
│   └── lua_scripts.go         # Atomic Lua scripts (EVALSHA, no round-trip races)
│
├── middleware/
│   └── middleware.go          # Chi-compatible HTTP middleware + key extractor helpers
│
├── handler/
│   └── handler.go             # Demo HTTP handlers (/health, /api/status, /admin/reset)
│
├── go.mod
└── go.sum
```

### Package responsibilities

|    Package   |                          Responsibility                             |
|--------------|---------------------------------------------------------------------|
| `config`     | Load all settings from environment variables with safe fallbacks    |
| `limiter`    | Define the `Limiter` interface and provide the Redis implementation |
| `middleware` | Wrap any `http.Handler` with rate-limit enforcement                 |
| `handler`    | Provide example route handlers to demonstrate the system            |

---

## Prerequisites

- **Go 1.21+**
- **Redis 6.0+** (Redis 7 recommended)
- Docker (optional, for running Redis locally)

---

## Getting Started

### 1. Clone and install dependencies

```bash
git clone https://github.com/themillenniumfalcon/drl
cd drl
go mod tidy
```

### 2. Start Redis

```bash
# Using Docker (recommended for local dev)
docker run -d --name drl-redis -p 6379:6379 redis:7-alpine

# Or, if Redis is already installed locally
redis-server
```

### 3. Run the server

```bash
go run .
```

You should see:

```
time=2024-02-16T10:00:00Z level=INFO msg="connected to Redis" addr=localhost:6379
time=2024-02-16T10:00:00Z level=INFO msg="server starting" port=8080
```

### 4. Test the rate limit

The default global rule allows **10 requests per minute** per IP. Send 12 requests quickly:

```bash
for i in {1..12}; do
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/api/status)
  echo "Request $i: HTTP $STATUS"
done
```

Expected output:

```
Request 1:  HTTP 200
Request 2:  HTTP 200
...
Request 10: HTTP 200
Request 11: HTTP 429
Request 12: HTTP 429
```

### 5. Inspect the rate-limit headers

```bash
curl -i http://localhost:8080/api/status
```

```
HTTP/1.1 200 OK
X-RateLimit-Limit:       10
X-RateLimit-Remaining:   7
X-RateLimit-Reset:       1708081260
X-RateLimit-Reset-Human: 2024-02-16T10:01:00Z
Content-Type: application/json
```

---

## Configuration

All settings are read from environment variables at startup. Every variable has a default so the service runs out of the box with no configuration.

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP port the server listens on |
| `REDIS_ADDR` | `localhost:6379` | Redis server address (`host:port`) |
| `REDIS_PASSWORD` | *(empty)* | Redis AUTH password |
| `REDIS_DB` | `0` | Redis logical database index |
| `RATE_LIMIT` | `10` | Default maximum requests per window |
| `RATE_WINDOW` | `1m` | Default window duration (Go duration string: `30s`, `1m`, `1h`) |
| `RATE_KEY_PREFIX` | `drl` | Prefix for all Redis keys (useful when sharing a Redis instance) |

### Example: stricter production settings

```bash
export PORT=9090
export REDIS_ADDR=redis.internal:6379
export REDIS_PASSWORD=supersecret
export RATE_LIMIT=100
export RATE_WINDOW=1m
export RATE_KEY_PREFIX=myapp

go run .
```

---

## API Reference

### `GET /health`

Health check. No rate limiting applied. Returns `200 OK` if the server is running.

```bash
curl http://localhost:8080/health
```

```json
{"status": "ok"}
```

---

### `GET /api/status`

Rate-limited by the **global rule** (IP-based, 10 req/min by default). Returns the current server time and a reminder to check headers.

```bash
curl -i http://localhost:8080/api/status
```

```json
{
  "status": "ok",
  "timestamp": "2024-02-16T10:00:00Z",
  "message": "Request successful. Check X-RateLimit-* headers."
}
```

---

### `GET /api/info`

Returns the effective rate-limit rule for your current request without consuming a token against your quota.

```bash
curl http://localhost:8080/api/info
```

```json
{
  "your_key":  "ip:127.0.0.1:54321",
  "rule_name": "global",
  "limit":     10,
  "window":    "1m0s",
  "note":      "This endpoint does NOT count against your rate limit."
}
```

---

### `POST /api/sensitive`

Rate-limited by the **strict rule** (API-key-based, 3 req/min). Requires the `X-API-Key` header; falls back to IP-based limiting if absent.

```bash
curl -X POST http://localhost:8080/api/sensitive \
     -H "X-API-Key: my-api-key-123"
```

```json
{"message": "sensitive operation completed"}
```

---

### `POST /admin/reset`

Clears all rate-limit counters for a given key. Useful in testing and support workflows. In production, protect this endpoint with authentication middleware.

```bash
curl -X POST http://localhost:8080/admin/reset \
     -H "Content-Type: application/json" \
     -d '{"key": "ip:127.0.0.1"}'
```

```json
{
  "reset": "ip:127.0.0.1",
  "status": "counters cleared"
}
```

**Request body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `key` | string | yes | The rate-limit key to clear (e.g. `ip:1.2.3.4`, `apikey:abc123`) |

---

### Rate-limited error response (`429`)

When the limit is exceeded all rate-limited endpoints return:

```
HTTP/1.1 429 Too Many Requests
Retry-After: 42
X-RateLimit-Limit:     10
X-RateLimit-Remaining: 0
X-RateLimit-Reset:     1708081260

rate limit exceeded
```

---

## Response Headers

Every request to a rate-limited endpoint receives these headers regardless of whether it was allowed:

| Header | Example | Description |
|---|---|---|
| `X-RateLimit-Limit` | `10` | Maximum requests allowed in the window |
| `X-RateLimit-Remaining` | `7` | Requests remaining in the current window |
| `X-RateLimit-Reset` | `1708081260` | Unix timestamp when the window resets |
| `X-RateLimit-Reset-Human` | `2024-02-16T10:01:00Z` | Same time in RFC 3339 format |
| `Retry-After` | `42` | Seconds to wait before retrying *(only on 429)* |

These headers follow the [draft-ietf-httpapi-ratelimit-headers](https://datatracker.ietf.org/doc/draft-ietf-httpapi-ratelimit-headers/) specification.

---

## Rate Limit Rules

A `Rule` combines a name, a maximum count, and a time window:

```go
limiter.Rule{
    Name:   "global",    // namespaces this rule's Redis key
    Limit:  100,         // max requests
    Window: time.Minute, // per window
}
```

Rules defined in `main.go`:

| Rule name | Limit | Window | Applied to | Keyed by |
|---|---|---|---|---|
| `global` | `RATE_LIMIT` (default 10) | `RATE_WINDOW` (default 1m) | `/api/status`, `/api/info` | Client IP |
| `strict` | 3 | 1 minute | `/api/sensitive` | API Key (falls back to IP) |

The Redis key format is: `<prefix>:<client_key>:<rule_name>`

For example: `drl:ip:192.168.1.1:global`

---

## Adding a New Rule

To add a new rate-limit rule (e.g. a per-tenant rule with a 5-minute window):

**Step 1** — Define the rule in `main.go`:

```go
tenantRule := limiter.Rule{
    Name:   "per-tenant",
    Limit:  500,
    Window: 5 * time.Minute,
}
```

**Step 2** — Write a key extractor:

```go
byTenantID := func(r *http.Request) string {
    return "tenant:" + r.Header.Get("X-Tenant-ID")
}
```

**Step 3** — Apply the middleware to the relevant route group:

```go
r.Group(func(r chi.Router) {
    r.Use(middleware.RateLimiter(lim, tenantRule, byTenantID))
    r.Post("/api/tenant/resource", myHandler)
})
```

That's all. The new rule gets its own Redis key namespace and does not interfere with any existing rules.

---

## Design Decisions

**Atomic Lua scripts over multi-command pipelines**

A naive implementation would send `INCR` and `EXPIRE` as two separate commands. Between those two commands, another server could also run `INCR` and set its own `EXPIRE`, causing the window to reset incorrectly. Executing both inside a Lua script makes Redis treat them as a single atomic operation.

**Fail open on Redis errors**

If Redis is unreachable, the middleware logs the error and *allows* the request to proceed rather than returning an error. This is a deliberate trade-off: an unhealthy rate limiter should not take your API offline. For services where this is unacceptable, change the error branch in `middleware/middleware.go` to return `503 Service Unavailable`.

**Interface-driven design**

`limiter.Limiter` is an interface, not a concrete type. This means:
- Unit tests can use a fast in-memory implementation with no Redis dependency.
- You can swap Redis for a different backend (Memcached, a database, etc.) without changing the middleware or handlers.
- Multiple limiter instances with different configs can coexist in the same binary.

**Per-rule key namespacing**

Redis keys include the rule name (`drl:<key>:<rule>`). This means the same client can be subject to multiple independent rules simultaneously — for example, a per-IP global limit and a per-API-key strict limit — without their counters interfering with each other.
