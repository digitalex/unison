"use strict";

const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const fs = require("node:fs");
const http = require("node:http");
const os = require("node:os");
const path = require("node:path");
const { test } = require("node:test");
const zlib = require("node:zlib");

const { assetName, ensureBinary, runBinary } = require("../lib/install.js");

const sha256 = (b) => crypto.createHash("sha256").update(b).digest("hex");
const SCRIPT = Buffer.from('#!/bin/sh\necho "args: $*"\nexit 4\n');
const GZ = zlib.gzipSync(SCRIPT);
const ASSET = "unison-linux-amd64.gz";

async function fixture(t, body = GZ) {
  const requests = [];
  const server = http.createServer((req, res) => {
    requests.push(req.url);
    res.end(body);
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const cache = fs.mkdtempSync(path.join(os.tmpdir(), "unison-cli-"));
  t.after(() => {
    server.close();
    fs.rmSync(cache, { recursive: true, force: true });
  });
  return {
    requests,
    opts: {
      version: "1.2.3",
      checksums: { [ASSET]: sha256(GZ) },
      platform: "linux",
      arch: "x64",
      env: { XDG_CACHE_HOME: cache },
      baseURL: `http://127.0.0.1:${server.address().port}`,
    },
  };
}

test("assetName matches what release.yml publishes", () => {
  assert.equal(assetName("darwin", "arm64"), "unison-darwin-arm64.gz");
  assert.equal(assetName("darwin", "x64"), "unison-darwin-amd64.gz");
  assert.equal(assetName("linux", "arm64"), "unison-linux-arm64.gz");
  assert.equal(assetName("linux", "x64"), ASSET);
  assert.equal(assetName("win32", "x64"), "unison-windows-amd64.exe.gz");
  assert.throws(() => assetName("win32", "arm64"), /no prebuilt binary/);
  assert.throws(() => assetName("freebsd", "x64"), /no prebuilt binary/);
});

test("downloads once, then uses the cache", async (t) => {
  const { requests, opts } = await fixture(t);
  const bin = await ensureBinary(opts);
  assert.deepEqual(fs.readFileSync(bin), SCRIPT);
  assert.deepEqual(requests, [`/v1.2.3/${ASSET}`]);
  assert.equal(await ensureBinary(opts), bin);
  assert.equal(requests.length, 1);
});

test("refuses a binary that fails the checksum and caches nothing", async (t) => {
  const { opts } = await fixture(t, zlib.gzipSync(Buffer.from("tampered")));
  await assert.rejects(ensureBinary(opts), /checksum mismatch/);
  const dir = path.join(opts.env.XDG_CACHE_HOME, "unison-cli", "1.2.3");
  assert.equal(fs.existsSync(dir), false);
});

test("refuses a platform with no published checksum", async (t) => {
  const { requests, opts } = await fixture(t);
  await assert.rejects(ensureBinary({ ...opts, checksums: {} }), /no checksum/);
  assert.equal(requests.length, 0);
});

test("passes on args and the exit code", { skip: process.platform === "win32" }, async (t) => {
  const { opts } = await fixture(t);
  const bin = await ensureBinary(opts);
  const { code, signal } = await runBinary(bin, ["wait", "--mentions"]);
  assert.equal(code, 4);
  assert.equal(signal, null);
});
