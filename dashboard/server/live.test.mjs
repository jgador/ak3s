import assert from "node:assert/strict";
import { createServer, request } from "node:http";
import { test } from "node:test";
import { snapshotMiddleware } from "./live.mjs";

test("local API reads snapshots, rejects cross-site and write requests, and hides failures", async () => {
  let calls = 0;
  const middleware = snapshotMiddleware(async () => { calls++; return { cluster: { name: "ak3s" } }; });
  const server = createServer((req, res) => middleware(req, res, () => { res.statusCode = 404; res.end(); }));
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  try {
    const base = `http://127.0.0.1:${server.address().port}`;
    const result = await fetch(`${base}/api/snapshot`);
    assert.equal(result.status, 200);
    assert.equal(result.headers.get("cache-control"), "no-store");
    assert.equal((await result.json()).cluster.name, "ak3s");
    assert.equal((await fetch(`${base}/api/snapshot`)).status, 200);
    assert.equal(calls, 1);
    for (const headers of [{ origin: "https://example.com" }, { host: "example.com" }, { "sec-fetch-site": "cross-site" }]) {
      const status = await new Promise((resolve, reject) => {
        request(`${base}/api/snapshot`, { headers }, (response) => {
          response.resume();
          resolve(response.statusCode);
        }).on("error", reject).end();
      });
      assert.equal(status, 403);
    }
    assert.equal((await fetch(`${base}/api/snapshot`, { method: "POST" })).status, 405);
    assert.equal((await fetch(`${base}/api/snapshot?config=/private/file`)).status, 400);
    assert.equal(calls, 1);
  } finally { await new Promise((resolve) => server.close(resolve)); }
  const failing = snapshotMiddleware(async () => { throw new Error("fictional-private-value"); });
  const failedServer = createServer((req, res) => failing(req, res, () => res.end()));
  await new Promise((resolve) => failedServer.listen(0, "127.0.0.1", resolve));
  try {
    const response = await fetch(`http://127.0.0.1:${failedServer.address().port}/api/snapshot`);
    assert.equal(response.status, 503);
    assert.ok(!(await response.text()).includes("fictional-private-value"));
  } finally { await new Promise((resolve) => failedServer.close(resolve)); }
});
