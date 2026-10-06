import { execFile } from "node:child_process";
import { existsSync, mkdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

const repo = fileURLToPath(new URL("../../", import.meta.url));
const executable = existsSync(`${repo}bin/ak3s`) ? `${repo}bin/ak3s` : "/usr/local/bin/ak3s";
const environment = {
  PATH: "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
  HOME: "/root",
  LANG: "C.UTF-8",
  TMPDIR: `${repo}.tmp`,
};

function collectSnapshot() {
  mkdirSync(environment.TMPDIR, { recursive: true, mode: 0o700 });
  const root = process.getuid?.() === 0;
  const command = root ? executable : "/usr/bin/sudo";
  const args = root
    ? ["dashboard-snapshot"]
    : ["-n", "--", "/usr/bin/env", "-i", ...Object.entries(environment).map(([key, value]) => `${key}=${value}`), executable, "dashboard-snapshot"];
  return new Promise((resolve, reject) => {
    execFile(command, args, { env: environment, timeout: 30_000, maxBuffer: 4 * 1024 * 1024 }, (error, stdout) => {
      // Never return child stderr, which may contain private host information.
      if (error) return reject(new Error("Cannot read the local cluster. Build the current AK3S CLI and check local kubeconfig permissions."));
      try {
        resolve(JSON.parse(stdout));
      } catch {
        reject(new Error("The local AK3S CLI returned invalid dashboard data."));
      }
    });
  });
}

function localRequest(req) {
  const remote = req.socket.remoteAddress;
  if (!["127.0.0.1", "::1", "::ffff:127.0.0.1"].includes(remote)) return false;
  let host;
  try { host = new URL(`http://${req.headers.host}`).hostname; } catch { return false; }
  if (!["localhost", "127.0.0.1", "[::1]"].includes(host)) return false;
  if (req.headers["sec-fetch-site"] && !["same-origin", "none"].includes(req.headers["sec-fetch-site"])) return false;
  if (req.headers.origin && req.headers.origin !== `http://${req.headers.host}`) return false;
  return true;
}

// One fixed, read-only route. No executable, arguments, cluster, or file path
// can be selected through browser input. Local host and origin checks prevent
// cross-site access to this development server's privileged projection.
export function snapshotMiddleware(collect = collectSnapshot) {
  let pending;
  let cached;
  let cachedAt = 0;
  return async (req, res, next) => {
    if (req.url?.split("?")[0] !== "/api/snapshot") return next();
    res.setHeader("Content-Type", "application/json");
    res.setHeader("Cache-Control", "no-store");
    res.setHeader("X-Content-Type-Options", "nosniff");
    const fail = (status, error) => { res.statusCode = status; res.end(JSON.stringify({ error })); };
    if (!localRequest(req)) return fail(403, "Local, same-origin access is required.");
    if (req.method !== "GET") { res.setHeader("Allow", "GET"); return fail(405, "Only read-only GET requests are supported."); }
    if (req.url !== "/api/snapshot") return fail(400, "This endpoint accepts no parameters.");
    try {
      if (!cached || Date.now() - cachedAt >= 5_000) {
        pending ??= collect().then((value) => { cached = value; cachedAt = Date.now(); return value; }).finally(() => { pending = undefined; });
        await pending;
      }
      res.end(JSON.stringify(cached));
    } catch {
      fail(503, "Cannot read the local cluster. Build the current AK3S CLI and check K3s and local kubeconfig permissions.");
    }
  };
}

export function liveClusterPlugin() {
  return {
    name: "ak3s-local-cluster",
    configureServer(server) { server.middlewares.use(snapshotMiddleware()); },
    configurePreviewServer(server) { server.middlewares.use(snapshotMiddleware()); },
  };
}
