# AGENTS.md

- Treat `README.md` as the single source of truth for product behavior, domain language, architecture, and scope. Update it before implementing a conflicting decision. Do not create competing design documents.
- Optimize the default journey for running `gnar` with no configuration. Keep self-hosting and operational concepts out of the normal client experience.
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
