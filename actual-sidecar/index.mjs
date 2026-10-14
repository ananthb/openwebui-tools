// SPDX-License-Identifier: AGPL-3.0-only
//
// A thin HTTP wrapper around the official @actual-app/api, for openwebui-tools.
// It owns the Actual server connection and a local sync copy; the Go MCP server
// proxies read calls to it. Env:
//
//   ACTUAL_SERVER_URL       e.g. https://actual.calculon.tech
//   ACTUAL_SERVER_PASSWORD  the Actual server password
//   ACTUAL_SYNC_ID          the budget's sync id (optional; first budget if unset)
//   ACTUAL_DATA_DIR         where the local copy lives (default /data)
//   PORT                    listen port (default 8091)

import http from 'node:http';
import * as api from '@actual-app/api';

const PORT = Number(process.env.PORT || 8091);
const DATA_DIR = process.env.ACTUAL_DATA_DIR || '/data';
const SERVER_URL = process.env.ACTUAL_SERVER_URL;
const PASSWORD = process.env.ACTUAL_SERVER_PASSWORD;
const SYNC_ID = process.env.ACTUAL_SYNC_ID || '';

let ready = false;

function log(...a) {
  console.error(new Date().toISOString(), ...a);
}

async function load() {
  await api.init({ dataDir: DATA_DIR, serverURL: SERVER_URL, password: PASSWORD });
  let syncId = SYNC_ID;
  if (!syncId) {
    const budgets = await api.getBudgets();
    if (budgets && budgets.length) {
      syncId = budgets[0].cloudFileId || budgets[0].id;
    }
  }
  if (!syncId) throw new Error('no budget found and ACTUAL_SYNC_ID unset');
  await api.downloadBudget(syncId);
  ready = true;
  log('ready, budget', syncId);
}

async function boot() {
  for (;;) {
    try {
      await load();
      break;
    } catch (e) {
      log('init failed, retrying:', e.message);
      await new Promise((r) => setTimeout(r, 5000));
    }
  }
  setInterval(
    () => api.sync().catch((e) => log('sync failed:', e.message)),
    5 * 60 * 1000,
  );
}

function send(res, code, body) {
  const b = JSON.stringify(body);
  res.writeHead(code, { 'content-type': 'application/json' });
  res.end(b);
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    let d = '';
    req.on('data', (c) => (d += c));
    req.on('end', () => {
      try {
        resolve(d ? JSON.parse(d) : {});
      } catch (e) {
        reject(e);
      }
    });
    req.on('error', reject);
  });
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://localhost');
  const p = url.pathname;
  if (p === '/health') return send(res, 200, { ok: ready });
  if (!ready) return send(res, 503, { error: 'not ready' });

  try {
    let m;
    if (req.method === 'GET' && p === '/accounts') {
      return send(res, 200, await api.getAccounts());
    }
    if (req.method === 'GET' && (m = p.match(/^\/accounts\/([^/]+)\/transactions$/))) {
      const since = url.searchParams.get('since') || '2000-01-01';
      const today = new Date().toISOString().slice(0, 10);
      return send(res, 200, await api.getTransactions(decodeURIComponent(m[1]), since, today));
    }
    if (req.method === 'GET' && (m = p.match(/^\/accounts\/([^/]+)\/balance$/))) {
      const id = decodeURIComponent(m[1]);
      if (typeof api.getAccountBalance === 'function') {
        return send(res, 200, { balance: await api.getAccountBalance(id) });
      }
      return send(res, 200, { balance: null });
    }
    if (req.method === 'GET' && (m = p.match(/^\/months\/([^/]+)\/categories$/))) {
      return send(res, 200, await api.getBudgetMonth(decodeURIComponent(m[1])));
    }
    if (req.method === 'POST' && p === '/query') {
      const body = await readBody(req);
      return send(res, 200, await api.runQuery(body.query));
    }
    send(res, 404, { error: 'not found' });
  } catch (e) {
    send(res, 500, { error: String(e && e.message ? e.message : e) });
  }
});

server.listen(PORT, () => log(`actual-sidecar on :${PORT}`));
boot();
