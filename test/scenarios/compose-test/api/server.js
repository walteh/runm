const http = require('http');

const server = http.createServer((req, res) => {
  res.writeHead(200, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify({
    message: 'Hello from the API service!',
    timestamp: new Date().toISOString(),
    service: 'api',
    env: process.env.NODE_ENV
  }));
});

const port = process.env.PORT || 3000;
server.listen(port, '0.0.0.0', () => {
  console.log(`API server running on port ${port}`);
});