import { afterEach, expect, test } from "bun:test";
import { spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const task = join(root, ".mise/tasks/release/tag");
const temporaryDirectories = [];

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true });
  }
});

function fixture() {
  const directory = mkdtempSync(join(tmpdir(), "actionlint-release-test-"));
  temporaryDirectories.push(directory);
  const repo = join(directory, "repo");
  const remote = join(directory, "remote.git");
  const bin = join(directory, "bin");
  mkdirSync(repo);
  mkdirSync(bin);
  mkdirSync(join(repo, ".changes"));
  writeFileSync(join(repo, "VERSION"), "1.9.0\n");
  writeFileSync(join(repo, ".changes/v1.9.0.md"), "Release notes\n");
  const env = {
    ...process.env,
    PATH: `${bin}:${process.env.PATH}`,
    GIT_CONFIG_NOSYSTEM: "1",
    GIT_CONFIG_GLOBAL: "/dev/null",
    GIT_CONFIG_COUNT: "5",
    GIT_CONFIG_KEY_0: "user.name",
    GIT_CONFIG_VALUE_0: "Release Test",
    GIT_CONFIG_KEY_1: "user.email",
    GIT_CONFIG_VALUE_1: "test@example.invalid",
    GIT_CONFIG_KEY_2: "commit.gpgSign",
    GIT_CONFIG_VALUE_2: "false",
    GIT_CONFIG_KEY_3: "tag.gpgSign",
    GIT_CONFIG_VALUE_3: "false",
    GIT_CONFIG_KEY_4: "core.hooksPath",
    GIT_CONFIG_VALUE_4: "/dev/null",
    RELEASE_TEST_LOG: join(directory, "preflight.log"),
  };
  writeFileSync(join(bin, "mise"), `#!/usr/bin/env bash
set -eu
[[ "$*" == 'run release:preflight' ]] || exit 90
printf 'preflight\n' >> "$RELEASE_TEST_LOG"
case "\${RELEASE_TEST_PREFLIGHT:-ok}" in
  fail) exit 1 ;;
  dirty) echo changed >> VERSION ;;
  move) git commit --allow-empty -m 'move HEAD' >/dev/null ;;
esac
`, { mode: 0o755 });

  const command = (program, args, overrides = {}) => spawnSync(program, args, {
    cwd: repo,
    env: { ...env, ...overrides },
    encoding: "utf8",
    timeout: 20000,
  });
  const git = (...args) => {
    const result = command("git", args);
    if (result.status !== 0) throw new Error(result.stderr || String(result.error));
    return result.stdout.trim();
  };
  git("init", "--bare", remote);
  git("init", "-b", "main");
  git("add", "VERSION", ".changes/v1.9.0.md");
  git("commit", "-m", "fixture");
  git("remote", "add", "origin", remote);
  git("push", "-u", "origin", "main");
  const release = (overrides = {}) => command("bash", [task], {
    usage_version: "1.9.0",
    usage_dry_run: "false",
    ...overrides,
  });
  const hasTag = () => command("git", ["show-ref", "--verify", "refs/tags/v1.9.0"]).status === 0;
  return { repo, remote, env, command, git, release, hasTag };
}

test("dry run completes preflight without creating or pushing a tag", () => {
  const f = fixture();
  const result = f.release({ usage_dry_run: "true" });
  expect(result.status).toBe(0);
  expect(result.stdout).toContain("Dry run:");
  expect(f.hasTag()).toBe(false);
  expect(f.git("ls-remote", "--tags", "origin")).toBe("");
  expect(readFileSync(f.env.RELEASE_TEST_LOG, "utf8")).toBe("preflight\n");
});

test("release pushes an annotated tag at the preflight commit", () => {
  const f = fixture();
  const before = f.git("rev-parse", "HEAD");
  expect(f.release().status).toBe(0);
  expect(f.git("cat-file", "-t", "v1.9.0")).toBe("tag");
  expect(f.git("rev-parse", "v1.9.0^{commit}")).toBe(before);
  expect(f.git("ls-remote", "--tags", "origin", "refs/tags/v1.9.0^{}")).toContain(before);
});

for (const version of ["01.9.0", "1.09.0", "1.9.00", "1.9.0-rc.1", "1.9.0;false"]) {
  test(`refuses invalid stable version ${version}`, () => {
    const f = fixture();
    const result = f.release({ usage_version: version });
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("invalid version");
    expect(f.hasTag()).toBe(false);
  });
}

for (const [name, prepare, message] of [
  ["non-main branch", f => f.git("switch", "-c", "feature"), "run from main"],
  ["dirty tree", f => writeFileSync(join(f.repo, "untracked"), "dirty"), "working tree must be clean"],
  ["unprepared VERSION", f => {
    writeFileSync(join(f.repo, "VERSION"), "1.8.0\n");
    f.git("commit", "-am", "old version");
    f.git("push");
  }, "prepare VERSION"],
  ["missing notes", f => {
    f.git("rm", ".changes/v1.9.0.md");
    f.git("commit", "-m", "remove notes");
    f.git("push");
  }, "release notes"],
  ["unpushed commit", f => f.git("commit", "--allow-empty", "-m", "unpushed"), "main must match"],
  ["existing local tag", f => f.git("tag", "v1.9.0"), "already exists"],
  ["existing remote tag", f => {
    f.git("tag", "v1.9.0");
    f.git("push", "origin", "refs/tags/v1.9.0");
    f.git("tag", "-d", "v1.9.0");
  }, "already exists"],
]) {
  test(`refuses ${name}`, () => {
    const f = fixture();
    prepare(f);
    const before = f.git("ls-remote", "--tags", "origin");
    const result = f.release();
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain(message);
    expect(f.git("ls-remote", "--tags", "origin")).toBe(before);
  });
}

for (const mode of ["fail", "dirty", "move"]) {
  test(`refuses preflight ${mode} without creating a tag`, () => {
    const f = fixture();
    const result = f.release({ RELEASE_TEST_PREFLIGHT: mode });
    expect(result.status).not.toBe(0);
    expect(f.hasTag()).toBe(false);
    expect(f.git("ls-remote", "--tags", "origin")).toBe("");
  });
}
