Playground for actionlint
=========================

This is a development directory for [actionlint playground](https://matcra587.github.io/actionlint/).

The playground is built with HTML/CSS/TypeScript/Wasm. Dependencies are defined in `package.json` and managed by Bun
with a committed `bun.lock`. Tasks for development are defined in [`.mise/tasks/tasks.toml`](../.mise/tasks/tasks.toml). Install
[mise](https://mise.jdx.dev/), then run `mise install --locked` to install the pinned tools, including Go and Bun.
Commands work from the repository root or this directory.

Bun bundles `index.html`, TypeScript imports, CSS imports, and referenced fonts into `dist/`.
Go builds `dist/main.wasm`; the Wasm task also supplies the matching `wasm_exec.js` for Bun to bundle.
Dependency installation has no build hooks. TypeScript checks types without emitting files.
The deployed site is static and does not require Bun.
The Bun build targets modern browsers; it does not downlevel JavaScript syntax to the TypeScript `target` setting.

## Tasks

```sh
# Install dependencies, build the playground, and serve it at localhost:1234
mise run playground:serve

# Install dependencies and build dist/
mise run playground:build

# Install dependencies
mise run playground:deps

# Run tests
mise run playground:test

# Remove dist/ and the generated Go runtime; retain node_modules
mise run playground:clean
```

## Lint

[Biome](https://biomejs.dev/) formats and lints TypeScript, JSON, and CSS. TypeScript separately checks types.
The mise task first ensures dependencies are installed:

```sh
mise run playground:lint
```

To format sources, run `bun run format` from this directory. `bun run watch` rebuilds the browser assets as sources
change; run `mise run playground:build` first to prepare the Go/Wasm files, then serve `dist/` in another terminal.

## Deployment

Run `mise run playground:deploy` to prepare a deployment using the [mise file task](../.mise/tasks/playground/deploy). See [CONTRIBUTING.md](../CONTRIBUTING.md) for more details.
To optimize `main.wasm`, `wasm-opt` command is required. Install [Binaryen](https://github.com/WebAssembly/binaryen) in
advance.
