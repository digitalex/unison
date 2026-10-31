"use strict";

// The npm package holds no binary: on first run it downloads the one for this
// platform from the GitHub release of the same version, checks it against the
// checksums published in the package, caches it, and from then on just runs it.
// Stdout belongs to the binary (agents parse --json), so progress goes to stderr.

const { spawn } = require("node:child_process");
const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const zlib = require("node:zlib");

const RELEASES = "https://github.com/digitalex/unison/releases/download";
const OSES = { darwin: "darwin", linux: "linux", win32: "windows" };
const ARCHES = { arm64: "arm64", x64: "amd64" };
const DOWNLOAD_TIMEOUT_MS = 120_000;

// assetName is the release file for a platform, as release.yml names it.
function assetName(platform, arch) {
  const goos = OSES[platform];
  const goarch = ARCHES[arch];
  if (!goos || !goarch || (goos === "windows" && goarch !== "amd64")) {
    throw new Error(`no prebuilt binary for ${platform}/${arch}; build from source with Go: https://github.com/digitalex/unison`);
  }
  return `unison-${goos}-${goarch}${goos === "windows" ? ".exe" : ""}.gz`;
}

function cacheDir(env, platform) {
  if (platform === "win32" && env.LOCALAPPDATA) return path.join(env.LOCALAPPDATA, "unison-cli");
  return path.join(env.XDG_CACHE_HOME || path.join(os.homedir(), ".cache"), "unison-cli");
}

// ensureBinary returns the path of the cached binary, downloading it if needed.
async function ensureBinary({ version, checksums, platform, arch, env, baseURL = RELEASES }) {
  const asset = assetName(platform, arch);
  const want = checksums[asset];
  if (!want) throw new Error(`this package has no checksum for ${asset}`);

  const dir = path.join(cacheDir(env, platform), version);
  const bin = path.join(dir, platform === "win32" ? "unison.exe" : "unison");
  if (fs.existsSync(bin)) return bin;

  const url = `${baseURL}/v${version}/${asset}`;
  console.error(`unison-cli: downloading ${asset} (v${version})...`);
  const resp = await fetch(url, { signal: AbortSignal.timeout(DOWNLOAD_TIMEOUT_MS) });
  if (!resp.ok) throw new Error(`downloading ${url} failed: HTTP ${resp.status}`);
  const gz = Buffer.from(await resp.arrayBuffer());
  const got = crypto.createHash("sha256").update(gz).digest("hex");
  if (got !== want) throw new Error(`checksum mismatch for ${asset}: refusing to run it`);

  // Rename into place, so a concurrent or interrupted run never sees half a binary.
  fs.mkdirSync(dir, { recursive: true });
  const tmp = `${bin}.${process.pid}.tmp`;
  fs.writeFileSync(tmp, zlib.gunzipSync(gz), { mode: 0o755 });
  fs.renameSync(tmp, bin);
  return bin;
}

// runBinary runs bin to completion, passing on its exit code: the protocol
// depends on them (`unison wait` exits 2 on a timeout, 4 when logged out).
function runBinary(bin, args) {
  return new Promise((resolve, reject) => {
    const child = spawn(bin, args, { stdio: "inherit" });
    const forward = (sig) => child.kill(sig);
    process.on("SIGINT", forward);
    process.on("SIGTERM", forward);
    child.on("error", reject);
    child.on("exit", (code, signal) => {
      process.off("SIGINT", forward);
      process.off("SIGTERM", forward);
      resolve({ code, signal });
    });
  });
}

async function main(args) {
  const { version } = require("../package.json");
  let checksums;
  try {
    checksums = require("../checksums.json");
  } catch {
    throw new Error("this copy has no checksums.json: only published packages can download a binary");
  }
  const bin = await ensureBinary({ version, checksums, platform: process.platform, arch: process.arch, env: process.env });
  const { code, signal } = await runBinary(bin, args);
  if (signal) process.kill(process.pid, signal);
  else process.exit(code);
}

module.exports = { assetName, ensureBinary, main, runBinary };
