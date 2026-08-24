# Security Policy

## Reporting a Vulnerability

We take security issues seriously. If you discover a vulnerability in dqex,
**do not open a public issue**. Please report it privately so it can be
investigated and fixed before disclosure.

Preferred channel: use GitHub's **private vulnerability reporting** on this
repository:

1. Go to https://github.com/fj1981/dqex/security/advisories
2. Click **New draft security advisory** and fill in the details.

Please include:

- The dqex version and platform affected
- A minimal reproduction (connection type, SQL/file involved — no real credentials)
- Impact description and any suggested fix, if you have one

## Response

- We will acknowledge your report within 5 business days.
- We will keep you informed about the investigation and fix progress.
- Once fixed, a security advisory and a patched release will be published.

## Scope

In scope: the dqex binary and its bundled Web UI, CLI commands, import/export
pipelines, snapshot handling, and AI module configuration handling.

Out of scope: vulnerabilities in third-party databases you connect to, or
misconfiguration of your own network access controls (`--host 0.0.0.0` etc.).
