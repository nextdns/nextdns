# AGENTS

## Project Context

- Project: NextDNS CLI (`github.com/nextdns/nextdns`)
- Language: Go (`go 1.26`)
- Purpose: Command-line DNS-over-HTTPS client focused on NextDNS, with support for other DoH providers and split-horizon setups.
- Target environments: Routers and Unix-like systems first, with support for macOS and Windows.

## Core Principles

1. Privacy first
   - Preserve user privacy by default.
   - Avoid adding telemetry or logging sensitive DNS/query data unless explicitly required.
2. Reliability and safety
   - Prefer stable behavior over clever optimizations.
   - Handle network failures and partial outages gracefully.
3. Cross-platform correctness
   - Keep behavior consistent across Linux, macOS, and Windows.
   - Use platform-specific code only when necessary and isolate it clearly.
4. Minimal, composable design
   - Keep features small and focused.
   - Avoid unnecessary dependencies and overly broad abstractions.
5. Backward compatibility
   - Preserve existing CLI behavior and flags unless a breaking change is intentional and documented.
   - Treat config format and runtime behavior as user-facing contracts.
6. Performance on constrained systems
   - Optimize for low memory/CPU use, especially router-like environments.
   - Assume deployments may have minuscule disk space; avoid features that increase on-device storage footprint.
   - Avoid expensive background work and unbounded caches.
7. Dependency and binary size discipline
   - Avoid new dependencies whenever possible; prefer stdlib/internal code when practical.
   - Be mindful of binary size and startup overhead when adding features.
   - If a dependency is unavoidable, justify it and keep transitive impact minimal.

## Agent Instructions

- Make the smallest safe change that solves the requested problem.
- Prefer edits that align with existing package boundaries (`proxy`, `resolver`, `router`, `config`, `host`, `ctl`, etc.).
- Keep operational behavior predictable:
  - no surprise network calls,
  - no hidden retries that block shutdown,
  - no silent fallback that changes security semantics.
- Be explicit about error handling:
  - return actionable errors,
  - avoid swallowing failures,
  - keep logs concise and useful.
- Preserve startup/shutdown behavior and service lifecycle semantics.
- When changing CLI/config behavior, update relevant docs in `README.md` and usage/help text if needed.

## Testing Guidance

- Run targeted tests for touched packages first, then broader tests when practical.
- Favor deterministic tests over timing-sensitive behavior.
- For networking changes, include failure-path coverage (timeouts, unreachable upstream, malformed responses).
- For platform-specific logic, ensure non-target platforms still compile cleanly.

## Dependency Checklist

Before adding a new dependency, verify:

- Can this be implemented with Go stdlib or existing internal packages?
- What is the transitive dependency impact (count and maintenance burden)?
- What is the expected binary size impact?
- Does it increase memory, disk, or startup-time footprint on router-class devices?
- Is there a simpler build-tag or optional-path approach for non-router targets?
- Is the trade-off documented in the PR/commit rationale?

## Binary Budget Note

- When introducing dependencies or feature-heavy code paths, include a before/after binary size comparison in the PR (or commit notes when no PR is used).
- Prefer disabling optional features by default when they materially increase size for router-class targets.

## Commit Tags

Use these commit tags to keep history consistent and searchable:

- `feat`: New user-facing functionality or capability.
  - Use when adding a new flag, command behavior, resolver/proxy feature, or supported integration.
- `bug`: Bug fix for incorrect behavior, regression, crash, or reliability issue.
  - Use when correcting behavior that was previously wrong, including edge-case and failure-path fixes.
- `chore`: Maintenance work that does not change user-facing behavior.
  - Use for refactors without behavior changes, tooling cleanups, docs-only updates, or test maintenance.
- `ci`: Continuous integration or release automation changes.
  - Use for workflow updates, build/release pipeline changes, and automation configuration.

Example commit subjects:

- `feat: add split-horizon fallback for custom upstreams`
- `bug: fix startup panic when cache directory is unavailable`
- `chore: simplify resolver config parsing without behavior change`
- `ci: add committer metadata to release workflow PR creation`

## Non-Goals for Routine Changes

- Large refactors without a clear user-facing benefit.
- New dependencies when existing stdlib/internal code is sufficient.
- Behavioral drift from existing defaults without explicit request.
