# Local gateway deployment

The MCP server remains bound to `127.0.0.1:18060`. A Docker-host gateway may
connect using the authority `host.docker.internal:18060`; MCP SDK 1.4 rejects
that authority by default when the server socket is loopback.

For that deployment only, set:

```
MCP_TRUSTED_LOCAL_HOST=host.docker.internal:18060
```

The exception is exact (including the port), applies only to a loopback local
socket, and does not use forwarded headers. Every other request retains the
SDK's default localhost protection. Existing bearer authentication still
applies. Leave the setting unset for ordinary direct localhost use.

Before cutover, test `tools/list` against an isolated candidate with the actual
gateway Host header; also verify that an unrelated Host is still rejected.
After restart, reconnect the existing gateway MCP client and verify login plus
a read through the gateway. A direct `/health` response alone is insufficient.

The current upstream binary uses its pinned bundled browser and no longer
accepts `-bin`. Prewarm and validate that browser before stopping the old
service. Pin the candidate's tested fingerprint seed with `XHS_FP_SEED` when
retaining a legacy cookie file; this prevents startup from creating a different
seed or rewriting the cookie envelope. Preserve the original cookie file's
permissions. Never print cookie values or copy them into this repository.

Back up the binary, startup configuration and cookies before replacing the
service. On rollback, restore a compatible binary/configuration/session set;
if the live cookies have changed since backup, reconcile them first rather
than blindly restoring an older account session. Reconnect the gateway after
any server restart. Keep its explicit tool allowlist unchanged when new
upstream tools are discovered.
