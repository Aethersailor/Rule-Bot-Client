# Rule-Bot Client design contract

> 本文面向审阅实现、打包和安全边界的开发者与维护者，记录不可破坏的设计约定。安装和使用 Rule-Bot Client 时，请从[项目 README](../README.md)和[用户 Wiki](https://github.com/Aethersailor/Rule-Bot-Client/wiki)开始；普通用户不需要按本文执行操作。

## Scope

Rule-Bot Client has one data path:

```text
controller log streams
  -> per-instance JSON and MATCH parser
  -> bounded candidate channel
  -> configured hostname or PSL eTLD+1 projection
  -> single-owner fingerprint set
  -> bounded write channel
  -> single buffered append writer
  -> optional durable-offset Rule-Bot sender
```

Version 1 deliberately excludes a GUI, database, metrics endpoint, hot reload,
per-instance output files, reverse DNS, and access-history sidecar logs.

## Runtime invariants

- One process manages all configured instances.
- Each instance owns one streaming HTTP request and reconnect state.
- Instance failures are isolated; output failures are fatal.
- All channels are bounded and Rule-Bot Client never intentionally drops a parsed
  candidate.
- Only the processor goroutine mutates the deduplication set.
- Domain projection happens before that set, so deduplication and persistence use
  the configured output identity rather than raw captured hostnames.
- Only the writer goroutine writes the output file.
- The Rule-Bot sender reads only through the writer's last synchronized byte offset.
- The output is exclusively locked on Linux.
- Shutdown stops readers, drains accepted candidates, flushes, and synchronizes
  the output before returning.
- Configuration changes take effect after a restart.

## Performance release gates

Initial release targets, measured on documented reference systems:

| Metric | Gate |
| --- | ---: |
| stripped static binary | 8 MiB maximum |
| compressed scratch image | 10 MiB maximum |
| one-instance idle RSS | 12 MiB maximum |
| additional idle instance | 0.5 MiB maximum |
| ten-minute idle CPU average | below 0.1% |
| 100,000-domain fingerprint set heap delta | 6 MiB maximum |
| 1,000,000-domain fingerprint set heap delta | 40 MiB maximum |

Throughput measurements on shared CI runners are recorded but are not hard
gates until a stable reference host is established. Allocation counts, binary
size, bounded-memory behavior, and functional tests are hard gates.

## Input and parsing

Rule-Bot Client requests `/logs?level=info` over a regular HTTP response stream. It
does not require WebSocket support. The parser recognizes only the exact final
rule forms currently emitted by compatible controllers:

```text
... --> example.com:443 match Match using ...
... (match Match/) ... --> example.com:443 error: ...
```

Merely containing the word `Match` is insufficient. Parser fixtures and fuzz
tests protect against rule names, proxy names, malformed JSON, oversized lines,
and future format drift.

After a stream reconnects, Rule-Bot Client requests one `/connections` snapshot and
submits active connections whose `rule` is exactly `Match`. This can recover
surviving long connections but not completed short connections.

TCP dialing and TLS handshakes are bounded. The `/logs` response-header wait and
long-lived response body intentionally have no separate deadline because Mihomo
may not flush HTTP headers until the first log record; an otherwise healthy but
quiet controller must not be forced into a reconnect loop. Connection and HTTP
failures use per-instance jittered exponential backoff; recovery logs include the
outage duration and number of failed attempts.

Hostnames are normalized to lowercase ASCII with IDNA Lookup processing before
projection. `registrable_domain` uses the bundled Public Suffix List, including
its PRIVATE section, and never performs a network lookup. Including private
suffixes preserves tenant boundaries such as `alice.github.io` versus
`bob.github.io`. The compatibility default remains `hostname` when an older
configuration omits `domain_mode`.

## Durability

The output file is append-only and opened with mode `0600`. A 64 KiB user-space
buffer is flushed and synchronized at the configured interval. Normal shutdown
performs a final flush and `fsync`. Rule-Bot Client does not periodically rewrite or
sort the file.

If a non-empty existing file lacks a final newline, Rule-Bot Client repairs it only
when the last line is itself a complete valid domain. An invalid partial tail is
reported and left untouched.

When Rule-Bot delivery is enabled, version-2 state contains a scan offset and
at most 1,024 deferred output offsets with their retry deadlines and backoff.
Version-1 checkpoints are read without discarding unsent records. The sender
never reads beyond the writer's latest successful `fsync`. Each scan advance
and its unresolved retry entry are committed together by atomic replacement
and directory synchronization. A full retry queue pauses scanning; it never
evicts unresolved records. Terminal acknowledgement removes the retry entry.
Fresh records and due retries alternate so neither can starve the other.
Transport, authentication and throttling failures pause the endpoint; an
application `temporary_error` is deferred independently. `Retry-After` and
endpoint pauses survive restarts. Retry metadata contains no domains or tokens.
This gives at-least-once delivery; Rule-Bot's duplicate check makes replay after
an acknowledgement/state-write crash safe. Downgrading requires restoring the
pre-upgrade checkpoint, which may safely replay already accepted domains.

A server-owned DNS deferral uses the legacy terminal `rejected_policy` status
and an additional `deferred=true` field. Old clients continue without upgrades;
new clients log `deferred_dns`. The server persists and retries that domain,
so the client must not create a second retry owner. Runtime status reports
`pending`, `unscanned_bytes` and `next_retry_at` for client-owned retries.

## Security

- Controller redirects are rejected so bearer credentials cannot cross hosts.
- Controller traffic ignores environment proxy variables and remains direct.
  Rule-Bot delivery is direct unless `proxy_url` or `proxy_from_environment` is
  explicitly configured. Redirects are rejected for both connections.
- Inline `secret` and `token` values are the default for ordinary deployments;
  protecting the main configuration file is sufficient.
- `secret_file`, `token_file`, `secret_env`, and `token_env` remain advanced
  injection and rotation sources. Each credential accepts exactly one source;
  conflicts are rejected instead of resolved by precedence.
- TLS verification is enabled by default. Custom roots and server names are
  supported per instance.
- Secrets are never included in logs or version output.
- Rule-Bot tokens can be reread from a file on every attempt for safe rotation.
- Container images run as a fixed non-root user, expose no port, and use a
  read-only root filesystem.

## Release identity

Every binary exposes its semantic version, source commit, build time, Go version,
and build target through `--version`. Linux, Windows, and OpenWrt Release assets
include SHA-256 checksums and GitHub artifact attestations. A machine-readable
client update manifest binds Linux and Windows targets to exact asset names,
sizes, hashes, and the release commit. Container images carry matching OCI
version and revision labels and are verified by digest and exact platform list
before a draft release is published.
