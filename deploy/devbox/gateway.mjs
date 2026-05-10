import { createReadStream, existsSync, statSync } from 'node:fs';
import { createServer, request as httpRequest } from 'node:http';
import { extname, join, normalize, resolve } from 'node:path';
import { pipeline } from 'node:stream';

const port = Number(process.env.DAPO_GATEWAY_PORT || 3040);
const staticRoot = resolve(process.env.DAPO_STATIC_ROOT || './public');
const apiUpstream = process.env.API_UPSTREAM || 'http://127.0.0.1:17180';
const adminUpstream = process.env.ADMIN_UPSTREAM || 'http://127.0.0.1:17188';
const openaiUpstream = process.env.OPENAI_UPSTREAM || 'http://127.0.0.1:17200';

const mime = new Map([
  ['.html', 'text/html; charset=utf-8'],
  ['.js', 'application/javascript; charset=utf-8'],
  ['.css', 'text/css; charset=utf-8'],
  ['.json', 'application/json; charset=utf-8'],
  ['.svg', 'image/svg+xml'],
  ['.png', 'image/png'],
  ['.jpg', 'image/jpeg'],
  ['.jpeg', 'image/jpeg'],
  ['.webp', 'image/webp'],
  ['.avif', 'image/avif'],
  ['.ico', 'image/x-icon'],
  ['.woff', 'font/woff'],
  ['.woff2', 'font/woff2'],
  ['.ttf', 'font/ttf']
]);

function safeJoin(root, urlPath) {
  const decoded = decodeURIComponent(urlPath.split('?')[0]);
  const clean = normalize(decoded).replace(/^(\.\.(\/|\\|$))+/, '');
  const full = resolve(join(root, clean));
  return full.startsWith(root) ? full : null;
}

function sendFile(res, path, noStore = false) {
  if (!path || !existsSync(path) || !statSync(path).isFile()) {
    res.writeHead(404, { 'content-type': 'text/plain; charset=utf-8' });
    res.end('not found');
    return;
  }
  const type = mime.get(extname(path).toLowerCase()) || 'application/octet-stream';
  res.writeHead(200, {
    'content-type': type,
    'cache-control': noStore ? 'no-store, no-cache, must-revalidate' : 'public, max-age=2592000, immutable'
  });
  createReadStream(path).pipe(res);
}

function proxy(req, res, upstream) {
  const target = new URL(req.url || '/', upstream);
  const headers = { ...req.headers, host: target.host };
  headers['x-forwarded-proto'] = req.headers['x-forwarded-proto'] || 'https';
  headers['x-forwarded-host'] = req.headers.host || '';

  const upstreamReq = httpRequest(target, { method: req.method, headers }, upstreamRes => {
    res.writeHead(upstreamRes.statusCode || 502, upstreamRes.headers);
    pipeline(upstreamRes, res, () => {});
  });
  upstreamReq.on('error', err => {
    res.writeHead(502, { 'content-type': 'text/plain; charset=utf-8' });
    res.end(`bad gateway: ${err.message}`);
  });
  pipeline(req, upstreamReq, () => {});
}

function spa(req, res, prefix, fallback) {
  const url = req.url || '/';
  const rel = prefix ? url.slice(prefix.length) : url;
  const file = safeJoin(staticRoot, prefix + rel);
  if (file && existsSync(file) && statSync(file).isFile()) {
    sendFile(res, file, false);
    return;
  }
  sendFile(res, join(staticRoot, fallback), true);
}

createServer((req, res) => {
  const url = req.url || '/';

  if (url.startsWith('/api/')) {
    proxy(req, res, apiUpstream);
    return;
  }
  if (url.startsWith('/admin/api/')) {
    proxy(req, res, adminUpstream);
    return;
  }
  if (url.startsWith('/v1/')) {
    proxy(req, res, openaiUpstream);
    return;
  }
  if (url === '/admin') {
    res.writeHead(301, { location: '/admin/' });
    res.end();
    return;
  }
  if (url.startsWith('/admin/')) {
    spa(req, res, '/admin', 'admin/index.html');
    return;
  }
  spa(req, res, '', 'index.html');
}).listen(port, '0.0.0.0', () => {
  console.log(`DAPO gateway listening on ${port}`);
});
