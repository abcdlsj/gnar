# AGENTS.md

## Core Principles

- User-first, out-of-the-box use, and minimal configuration are the highest priorities. `gnar` should work with no configuration whenever possible; prefer sensible defaults, automatic discovery, and progressive disclosure of advanced options. Keep self-hosting and operational concepts out of the normal client path.
- Release binary size is a core engineering quality metric. Assess the impact of every new dependency, feature, protocol, or build setting on each release target; measure and record release artifacts before handoff, and prefer small, maintained dependencies with only the features that are needed. Never trade supported behavior, security, or reliability for a smaller binary.

- Treat `README.md` as the single source of truth for product behavior, domain language, architecture, and scope. Update it before implementing a conflicting decision. Do not create competing design documents.
- Keep the product HTTP-first and interaction-first. Complete the HTTP request-inspection experience before adding protocols or administration features.
- Follow robpick guidance: use precise domain names, keep cohesive code together, prefer small interfaces, and make state ownership obvious.
- Prefer a maintained third-party library when it removes non-product infrastructure. Keep dependencies focused and behind narrow local boundaries; avoid overlapping libraries.
- Start with one Rust crate and one binary. Split modules or crates only at demonstrated ownership or build boundaries.
- Prefer concrete types. Add traits only for an actual alternate implementation or a useful test boundary.
- Model concurrency with owned tasks, bounded channels, explicit cancellation, and backpressure. Do not share mutable transport or UI state casually.
- Keep the TUI downstream of application events. TUI, plain text, and JSON output must not implement separate product behavior.
- Keep secrets, request bodies, and response bodies out of logs and SQLite. Bound all captured traffic in memory and redact common credentials by default.
- Return errors with operational context and a useful next action. Do not collapse discovery, authentication, edge, and local-upstream failures into one message.
- Do not add code comments unless tooling or the language requires them. Make names and structure carry intent.
- Add behavior through vertical slices with proportionate unit and end-to-end coverage. Keep the repository runnable after each slice.
