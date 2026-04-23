# Security policy

## Reporting a vulnerability

If you discover a security issue in rakit, please **do not** open a public
GitHub issue. Instead, email the maintainers at
`security@ratrektlabs.com` (or open a GitHub private vulnerability report via
the "Security" tab on this repository).

Please include:

- A description of the vulnerability and its impact.
- Steps to reproduce, ideally with a minimal example.
- Any suggested mitigation.

We aim to acknowledge reports within 3 business days and to provide a fix or
mitigation within 30 days for high-severity issues.

## Supported versions

rakit is pre-1.0; we support the latest `main` branch. Once a stable release
is cut, this document will be updated with a support matrix.

## Scope

rakit is a framework — security issues in user-written skills, MCP servers, or
custom providers are out of scope unless they are caused by a defect in rakit
itself. Examples of in-scope issues:

- Session data leaking across users or agents.
- Path traversal in the blob store.
- Prompt/tool injection vectors that bypass documented sandboxing.
- Denial of service via malformed provider or MCP messages.

## Handling of secrets

rakit does not log API keys, user messages, or any other sensitive data by
default. If you observe rakit logging something it should not, please treat
it as a security issue.
