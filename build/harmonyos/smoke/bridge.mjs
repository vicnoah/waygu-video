import fs from 'node:fs';
import net from 'node:net';
import path from 'node:path';
import vm from 'node:vm';

const web = process.argv[2];
if (!web) throw new Error('Pass the web directory from the HarmonyOS smoke build');
const streams = new Map();
const listeners = new Map();
let child = 0;
let running = true;
const event = (kind, id, number = 0, data = '') => {
  setImmediate(() => {
    if (running) globalThis.wayguTCPEvent(kind, id, number, data);
  });
};

function attach(id, socket) {
  const stream = { socket, waiting: false, ended: false };
  streams.set(id, stream);
  socket.on('readable', () => flush(id));
  socket.on('end', () => {
    stream.ended = true;
    flush(id);
  });
  socket.on('error', () => event('error', id, 2300007));
  return stream;
}

function flush(id) {
  const stream = streams.get(id);
  if (!stream?.waiting) return;
  const socket = stream.socket;
  const available = Math.min(socket.readableLength, 32 * 1024);
  if (available > 0) {
    const part = socket.read(available);
    if (!part) return;
    stream.waiting = false;
    event('chunk', id, 0, part.toString('base64'));
  } else if (stream.ended) {
    stream.waiting = false;
    event('eof', id);
  }
}

globalThis.nsgoHarmony = {
  wayguTCPDial(id, host, port, network) {
    const family = network === 'tcp6' ? 6 : network === 'tcp4' ? 4 : 0;
    const socket = net.createConnection({ host, port, family });
    attach(id, socket);
    socket.on('connect', () => event('ready', id, 0, socket.localAddress + ':' + socket.localPort));
    return true;
  },
  wayguTCPListen(id) {
    const server = net.createServer(socket => {
      const remote = --child;
      attach(remote, socket);
      event('accept', id, remote, socket.remoteAddress + ':' + socket.remotePort);
    });
    listeners.set(id, server);
    server.on('error', () => event('error', id, 2300002));
    server.listen(0, '127.0.0.1', () => event('ready', id, server.address().port));
    return true;
  },
  wayguTCPPull(id) {
    const stream = streams.get(id);
    if (!stream) return false;
    stream.waiting = true;
    flush(id);
    return true;
  },
  wayguTCPSend(id, text) {
    const stream = streams.get(id);
    if (!stream) return false;
    stream.socket.write(Buffer.from(text, 'base64'), err => {
      if (err) event('error', id, 2300002);
      else event('sent', id);
    });
    return true;
  },
  wayguTCPClose(id) {
    streams.get(id)?.socket.destroy();
    streams.delete(id);
    listeners.get(id)?.close();
    listeners.delete(id);
    return true;
  },
  wayguTCPVerify(id) {
    // The smoke HTTPS endpoint uses an untrusted fixture certificate.
    event('verify', id, 19030005);
    return true;
  },
};

vm.runInThisContext(fs.readFileSync(path.join(web, 'wasm_exec.js'), 'utf8'));
const go = new globalThis.Go();
go.env.WAYGU_TEST_PASSWORD = process.env.WAYGU_TEST_PASSWORD ?? '';
go.exit = code => {
  running = false;
  process.exit(code);
};
const wasm = await WebAssembly.instantiate(fs.readFileSync(path.join(web, 'app.wasm')), go.importObject);
const timer = setTimeout(() => {
  console.error('HarmonyOS Wasm smoke timed out');
  process.exit(1);
}, 90000);
try {
  await go.run(wasm.instance);
} finally {
  clearTimeout(timer);
  for (const id of streams.keys()) globalThis.nsgoHarmony.wayguTCPClose(id);
  for (const id of listeners.keys()) globalThis.nsgoHarmony.wayguTCPClose(id);
}
