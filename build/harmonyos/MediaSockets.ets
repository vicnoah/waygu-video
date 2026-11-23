import { connection, socket } from '@kit.NetworkKit';
import { cert } from '@kit.DeviceCertificateKit';
import { util } from '@kit.ArkTS';
import type { BusinessError } from '@kit.BasicServicesKit';

type Notify = (kind: string, id: number, number: number, data: string) => void;
const CHUNK = 32 * 1024;
const BUFFER_LIMIT = 16 * 1024 * 1024;

class Stream {
  send: ((options: socket.TCPSendOptions) => Promise<void>) | undefined;
  close: (() => Promise<void>) | undefined;
  queue: Uint8Array[] = [];
  offset: number = 0;
  bytes: number = 0;
  waiting: boolean = false;
  ended: boolean = false;
  error: number = 0;
}

function addressText(address: connection.NetAddress): string {
  const host = address.address.includes(':') ? '[' + address.address + ']' : address.address;
  return host + ':' + String(address.port ?? 0);
}

// The hidden engine page requests one chunk at a time. The renderer never
// queues multiple JavaScript callbacks with copies of the same stream data.
export class MediaSockets {
  private streams: Map<number, Stream> = new Map();
  private listeners: Map<number, socket.TCPSocketServer> = new Map();
  private childID: number = 0;
  private codec: util.Base64Helper = new util.Base64Helper();
  private notify: Notify;

  constructor(notify: Notify) {
    this.notify = notify;
  }

  dial(id: number, host: string, port: number, network: string): boolean {
    const state = new Stream();
    this.streams.set(id, state);
    this.connect(id, state, host, port, network);
    return true;
  }

  private async connect(id: number, state: Stream, host: string, port: number, network: string): Promise<void> {
    let code = 2300007;
    try {
      let addresses: connection.NetAddress[];
      if (host.includes(':')) {
        addresses = [{ address: host, family: 2 }];
      } else if (/^\d+\.\d+\.\d+\.\d+$/.test(host)) {
        addresses = [{ address: host, family: 1 }];
      } else {
        addresses = await connection.getAddressesByName(host);
      }
      for (const remote of addresses) {
        if (this.streams.get(id) !== state) {
          return;
        }
        if ((network === 'tcp4' && remote.family === 2) || (network === 'tcp6' && remote.family !== 2)) {
          continue;
        }
        const client = socket.constructTCPSocketInstance();
        state.close = () => client.close();
        let connected = false;
        client.on('message', (message: socket.SocketMessageInfo) => this.receive(id, message.message));
        client.on('close', () => {
          if (connected) {
            this.end(id, 0);
          }
        });
        client.on('error', (error: BusinessError) => {
          if (connected) {
            this.end(id, error.code);
          }
        });
        try {
          await client.connect({
            address: { address: remote.address, family: remote.family, port: port },
            timeout: 8000
          });
        } catch (error) {
          code = (error as BusinessError).code;
          client.close().catch(() => {});
          continue;
        }
        if (this.streams.get(id) !== state) {
          client.close().catch(() => {});
          return;
        }
        connected = true;
        state.send = options => client.send(options);
        let local = '';
        try {
          local = addressText(await client.getLocalAddress());
        } catch {}
        if (this.streams.get(id) === state) {
          this.notify('ready', id, 0, local);
        }
        return;
      }
    } catch (error) {
      code = (error as BusinessError).code;
    }
    this.fail(id, code);
  }

  listen(id: number): boolean {
    try {
      const server = socket.constructTCPSocketServerInstance();
      this.listeners.set(id, server);
      server.on('connect', (client: socket.TCPSocketConnection) => this.accept(id, server, client));
      server.on('error', (error: BusinessError) => {
        if (this.listeners.get(id) === server) {
          this.notify('error', id, error.code, '');
          this.close(id);
        }
      });
      this.startListener(id, server);
      return true;
    } catch {
      return false;
    }
  }

  private async startListener(id: number, server: socket.TCPSocketServer): Promise<void> {
    try {
      await server.listen({ address: '127.0.0.1', family: 1, port: 0 });
      const local = await server.getLocalAddress();
      if (this.listeners.get(id) === server && (local.port ?? 0) > 0) {
        this.notify('ready', id, local.port ?? 0, '');
        return;
      }
      if (this.listeners.get(id) === server) {
        this.notify('error', id, 2300002, '');
        this.close(id);
      } else {
        await server.close();
      }
    } catch (error) {
      if (this.listeners.get(id) === server) {
        this.notify('error', id, (error as BusinessError).code, '');
        this.close(id);
      }
    }
  }

  private async accept(listener: number, server: socket.TCPSocketServer,
    client: socket.TCPSocketConnection): Promise<void> {
    if (this.listeners.get(listener) !== server) {
      client.close().catch(() => {});
      return;
    }
    const id = --this.childID;
    const state = new Stream();
    state.send = options => client.send(options);
    state.close = () => client.close();
    this.streams.set(id, state);
    client.on('message', (message: socket.SocketMessageInfo) => this.receive(id, message.message));
    client.on('close', () => this.end(id, 0));
    client.on('error', (error: BusinessError) => this.end(id, error.code));
    let remote = '';
    try {
      remote = addressText(await client.getRemoteAddress());
    } catch {}
    if (this.listeners.get(listener) === server) {
      this.notify('accept', listener, id, remote);
    } else {
      this.close(id);
    }
  }

  pull(id: number): boolean {
    const state = this.streams.get(id);
    if (state === undefined) {
      return false;
    }
    state.waiting = true;
    this.flush(id, state);
    return true;
  }

  private receive(id: number, data: ArrayBuffer): void {
    const state = this.streams.get(id);
    if (state === undefined || state.ended || data.byteLength === 0) {
      return;
    }
    if (state.bytes + data.byteLength > BUFFER_LIMIT) {
      this.end(id, 2300027);
      state.close?.().catch(() => {});
      return;
    }
    state.queue.push(new Uint8Array(data));
    state.bytes += data.byteLength;
    this.flush(id, state);
  }

  private end(id: number, error: number): void {
    const state = this.streams.get(id);
    if (state !== undefined && !state.ended) {
      state.ended = true;
      state.error = error;
      this.flush(id, state);
    }
  }

  private flush(id: number, state: Stream): void {
    if (!state.waiting) {
      return;
    }
    if (state.queue.length !== 0) {
      const first = state.queue[0];
      const part = first.subarray(state.offset, state.offset + CHUNK);
      state.offset += part.length;
      state.bytes -= part.length;
      if (state.offset === first.length) {
        state.queue.shift();
        state.offset = 0;
      }
      state.waiting = false;
      this.notify('chunk', id, 0, this.codec.encodeToStringSync(part));
    } else if (state.ended) {
      state.waiting = false;
      this.notify(state.error === 0 ? 'eof' : 'error', id, state.error, '');
    }
  }

  send(id: number, data: string): boolean {
    const state = this.streams.get(id);
    const write = state?.send;
    if (state === undefined || write === undefined || state.ended) {
      return false;
    }
    try {
      const bytes = this.codec.decodeSync(data);
      const payload = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
      write({ data: payload }).then(() => {
        if (this.streams.get(id) === state) {
          this.notify('sent', id, 0, '');
        }
      }).catch((error: BusinessError) => this.fail(id, error.code));
      return true;
    } catch (error) {
      this.fail(id, (error as BusinessError).code);
      return true;
    }
  }

  private fail(id: number, code: number): void {
    if (this.streams.has(id)) {
      this.notify('error', id, code || 2300002, '');
      this.close(id);
    }
  }

  verify(id: number, hostname: string, data: string): boolean {
    this.verifyChain(id, hostname, data);
    return true;
  }

  private async verifyChain(id: number, hostname: string, data: string): Promise<void> {
    try {
      const certificates = JSON.parse(data) as string[];
      const chain: cert.X509Cert[] = [];
      for (const record of certificates) {
        chain.push(await cert.createX509Cert({ data: this.codec.decodeSync(record), encodingFormat: cert.EncodingFormat.FORMAT_DER }));
      }
      await cert.createX509CertChain(chain).validate({
        trustAnchors: [],
        trustSystemCa: true,
        policy: cert.ValidationPolicyType.VALIDATION_POLICY_TYPE_SSL,
        sslHostname: hostname
      });
      this.notify('verify', id, 0, '');
    } catch (error) {
      this.notify('verify', id, (error as BusinessError).code || 19030001, '');
    }
  }

  close(id: number): boolean {
    const state = this.streams.get(id);
    if (state !== undefined) {
      this.streams.delete(id);
      state.close?.().catch(() => {});
    }
    const server = this.listeners.get(id);
    if (server !== undefined) {
      this.listeners.delete(id);
      server.close().catch(() => {});
    }
    return true;
  }
}
