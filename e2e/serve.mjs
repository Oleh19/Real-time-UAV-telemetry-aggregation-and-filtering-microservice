import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join, normalize } from 'node:path';

const dist = process.env.DIST || '../frontend/dist/uav-dashboard/browser';
const port = Number(process.env.PORT || 4173);

const contentTypes = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.json': 'application/json',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon',
  '.png': 'image/png',
  '.woff2': 'font/woff2',
};

createServer(async (req, res) => {
  const requestPath = decodeURIComponent((req.url || '/').split('?')[0]);
  let filePath = join(dist, normalize(requestPath));
  let body;
  try {
    body = await readFile(filePath);
  } catch {
    filePath = join(dist, 'index.html');
    body = await readFile(filePath);
  }
  res.writeHead(200, {
    'content-type': contentTypes[extname(filePath)] || 'application/octet-stream',
  });
  res.end(body);
}).listen(port, () => {
  console.log(`serving ${dist} on ${port}`);
});
