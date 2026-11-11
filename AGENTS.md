# EasyCR working rules

- Use the existing Go backend and embedded plain HTML/CSS/JavaScript console.
- Run `make build` to embed asset changes; use `make verify` for Go verification.
- Run `npm ci`, `npx playwright install chromium`, and `make test-ui` for UI checks.
- Run `make test-e2e` only with a working Docker daemon; it exercises real images.
- Follow [container build instructions](docs/build.md) for Compose and local env setup.
- Format Go with gofmt; use lowercase package names and snake_case Go filenames.
- Keep Go imports grouped by standard library and project/dependency imports.
- Return or handle errors explicitly, propagate request contexts, and close resources.
- Validate HTTP inputs and preserve authentication, CSRF, and storage locking rules.
- Match existing JavaScript double quotes, semicolons, and camelCase names.
- Use native accessible controls and DOM text APIs for untrusted values.
- Use self-explanatory names and cohesive functions; avoid duplication and unnecessary abstractions.
- Comment only non-obvious constraints or decisions; preserve legal/tooling directives.
- Keep documentation in English and update affected usage/configuration docs with changes.
