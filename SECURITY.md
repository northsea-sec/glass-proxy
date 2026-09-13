# Security

## Reporting a vulnerability

Use [GitHub private vulnerability reporting](https://github.com/northsea-sec/glass-proxy/security/advisories/new) for sensitive reports. Include the affected source revision, relevant route/configuration, impact, and a minimal reproduction using synthetic inputs.

Do not place provider credentials, live prompts, conversation archives, database contents, or exploitable deployment details in a public issue. If private reporting is unavailable, open a public issue requesting a private contact channel without disclosing the vulnerability details.

## Deployment boundary

Glass is a single-user proxy, not a hosted multi-tenant service or a credential broker. It processes provider authentication and conversation content and exposes diagnostic/control endpoints. Some control operations can forward requests using captured credentials.

The binary's default TCP listen address is not restricted to loopback. Production wrappers also contain workstation-specific listener and path choices. Choose an explicit private listener and appropriate network access controls before deployment; do not expose the proxy or its debug/guard endpoints to untrusted clients.

An optional Unix-domain socket is supported by the process, but selecting it does not automatically disable the TCP listener. Review listener assembly and deployment controls together.

## Sensitive state

Protect runtime roots and capture destinations with restrictive filesystem access. They may contain:

- provider request templates and system prompts;
- canonical cache snapshots and session metadata;
- shadows, chapter files, and recovery summaries;
- diagnostic databases, telemetry, and logs;
- optional HTTP/WebSocket captures.

Plaintext state and file creation modes in the retained implementation require deployment-level protection. Do not rely on ignore rules, a redacted debug status response, or a single scanner as a confidentiality boundary. Logs and captures may contain sensitive material even when credentials are omitted from a particular record.

Keep credentials out of command arguments, public issues, source files, and publication artifacts. Do not reuse an existing production runtime root for an independent installation or experiment.

## Guard scope

The embedded security guard and optional scanners are retained from production. Their configuration, availability, and call sites determine coverage; they are not a universal sandbox or a guarantee that every provider route is filtered. Review the specific route rather than inferring coverage from the presence of a guard package.

## Release scope

This publication preserves the existing production implementation. It does not introduce security hardening or claim a new security audit. Source-copy and remote-publication checks are static; no Glass execution or runtime-state inspection is part of the release process.
