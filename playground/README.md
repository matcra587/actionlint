Playground for actionlint
=========================

This is a development directory for [actionlint playground](https://matcra587.github.io/actionlint/).

The playground is built with HTML/CSS/TypeScript/Wasm. All dependencies are defined in `package.json` and managed by `npm`.
Tasks for development are defined in [`tasks.toml`](../tasks.toml). Install [mise](https://mise.jdx.dev/), Go, Node.js, and npm
before running them. Commands work from the repository root or this directory.

## Tasks

```sh
# Install dependencies, build the playground, and serve it at localhost:1234
mise run playground:serve

# Install dependencies, build main.wasm
mise run playground:build

# Install dependencies
mise run playground:deps

# Run tests
mise run playground:test

# Remove main.wasm, index.js, index.js.map, and copied assets; retain node_modules
mise run playground:clean
```

## Lint

Sources are linted with [eslint](https://eslint.org/) with [typescript-eslint](https://github.com/typescript-eslint/typescript-eslint),
[prettier](https://prettier.io/) and [stylelint](https://stylelint.io/).

The `lint` npm script applies all the linters. The mise task first ensures dependencies are installed:

```sh
mise run playground:lint
```

## Deployment

Deployment is automated by [`deploy.bash`](./deploy.bash). See [CONTRIBUTING.md](../CONTRIBUTING.md) for more details.
To optimize `main.wasm`, `wasm-opt` command is required. Install [Binaryen](https://github.com/WebAssembly/binaryen) in
advance.
