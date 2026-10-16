# Operator contract

Execute only when existing authorization covers this target, model, and cost
scope; do not ask again merely because the command is about to run. The helper takes
an absolute trusted `--aiqa` binary, exactly one target, base URL, and model.
It reads only the selected canonical credential from its inherited environment:
`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, or `GOOGLE_GENAI_API_KEY`. It never
accepts a key argument and does not use aliases.

The URL must be HTTPS, except `http` for `localhost` or a numeric loopback
address used in local tests. Userinfo, query, fragment, control characters, and
oversized URLs are rejected. Models are one bounded ID with no comma or control
characters. Google profiles are limited to the embedded GenerateContent default
or the reviewed Interactions profile.

Execution creates a unique private directory and passes an intentionally small
environment: `PATH`, `LANG`, `LC_ALL`, `TZ`, the one selected key, and its
canonical base-URL variable. It
removes HOME, proxy, OTEL, loader, extension, and other credential variables.
Artifacts and captured stdout/stderr are private. The helper records only
redacted metadata in its JSON response and never echoes captured output.

The fixed AIQA invocation has `test=compat` (optionally built-in `sdk`), one
target/model, JSON output, 30-second request timeout, 300-second run timeout,
30-second queue timeout, concurrency and target concurrency of one, one-second
request interval, 256 catalog entries, one selected model, and 1 MiB inventory.
It does not request artifacts, high-token or persistent probes, model-parameter
tests, or extensions. An outer 310-second process guard is also enforced.

The helper checks that the selected POSIX binary itself is not group- or
world-writable. The operator remains responsible for trusting its resolved
binary path, interpreter, dependencies, and ancestor directories; the helper
does not claim to validate them. It does not claim equivalent Windows ACL
enforcement. On Windows, use WSL with a POSIX-visible trusted binary and
private artifact directory; do not bypass these checks with a purported manual
ACL equivalent.
