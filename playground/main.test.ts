import { beforeAll, describe, it } from 'bun:test';
import { strict as assert } from 'node:assert';
import { JSDOM } from 'jsdom';
// Inject global.Go for testing `main.wasm`.
import './wasm_exec.js';

class CheckResults {
    errors: ActionlintError[] | null = null;
    resolve: ((errs: ActionlintError[]) => void) | null = null;

    onCheckCompleted(errs: ActionlintError[]) {
        this.errors = errs;
        if (this.resolve !== null) {
            this.resolve(errs);
            this.resolve = null;
        }
    }

    waitCheckCompleted(): Promise<ActionlintError[]> {
        return new Promise(resolve => {
            if (this.errors !== null) {
                resolve(this.errors);
                return;
            }
            this.resolve = resolve;
        });
    }

    reset(): void {
        this.errors = null;
    }
}

describe('main.wasm', () => {
    const results = new CheckResults();

    beforeAll(async () => {
        const dom = new JSDOM('');
        dom.window.dismissLoading = () => {
            /*do nothing*/
        };
        dom.window.getYamlSource = () => `
on: push

jobs:
  test:
    steps:
      - run: echo 'hi'`;
        dom.window.onCheckCompleted = results.onCheckCompleted.bind(results);

        global.window = dom.window as unknown as Window & typeof globalThis;

        const go = new Go();
        const buf = await Bun.file(new URL('./dist/main.wasm', import.meta.url)).arrayBuffer();
        const result = await WebAssembly.instantiate(buf, go.importObject);

        // Do not `await` this method call since it will never be settled
        void go.run(result.instance);
    });

    it('shows first result on loading', async () => {
        const errors = await results.waitCheckCompleted();

        const json = JSON.stringify(errors);
        assert.equal(errors.length, 1, json);

        const err = errors[0];
        assert.ok(err);
        assert.equal(err.message, '"runs-on" section is missing in job "test"', `message is unexpected: ${json}`);
        assert.equal(err.line, 5, `line is unexpected: ${json}`);
        assert.equal(err.column, 3, `column is unexpected: ${json}`);
        assert.equal(err.kind, 'syntax-check', `kind is unexpected: ${json}`);
    });

    it('reports some errors by running actionlint with runActionlint', async () => {
        assert.ok(window.runActionlint);
        results.reset();

        const source = `
on: foo

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo 'hi'`;

        window.runActionlint(source);
        const errors = await results.waitCheckCompleted();
        const json = JSON.stringify(errors);
        assert.equal(errors.length, 1, json);

        const err = errors[0];
        assert.ok(err);
        assert.ok(err.message.includes('unknown Webhook event "foo"'), `message is unexpected: ${json}`);
        assert.equal(err.line, 2, `line is unexpected: ${json}`);
        assert.equal(err.column, 5, `column is unexpected: ${json}`);
        assert.equal(err.kind, 'events', `kind is unexpected: ${json}`);
    });

    it('reports no error by running actionlint with runActionlint', async () => {
        assert.ok(window.runActionlint);
        results.reset();

        const source = `
on: push

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo 'hi'`;

        window.runActionlint(source);
        const errors = await results.waitCheckCompleted();
        const json = JSON.stringify(errors);
        assert.equal(errors.length, 0, json);
    });
});
