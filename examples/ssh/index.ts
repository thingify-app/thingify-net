import { createResponderConfig, InsecureServerAuth, Listeners, ThingPeer } from 'thingrtc-peer';
import { Terminal } from 'xterm';

interface SSHConnection {
    connect(host: string, username: string, password: string): void;
    readFromTerminal(str: string): void;
    readFromNetwork(buf: Uint8Array): void;
}

const SIGNALLING_SERVER_URL = 'wss://thingify.deno.dev/signalling';
const REMOTE_HOST = '10.0.1.1';
const BUFFER_SIZE_BYTES = 16384;

function callFuncWithStrings(func: (...args: any[]) => any, memory: ArrayBuffer, mallocFunc: (n: number) => number, ...args: string[]) {
    const funcArgs = [];
    const encoder = new TextEncoder();
    for (const arg of args) {
        const encoded = encoder.encode(arg);
        const addr = mallocFunc(encoded.length);
        const buf = new Uint8Array(memory, addr, encoded.length);
        buf.set(encoded);
        funcArgs.push(addr);
        funcArgs.push(arg.length);
    }
    func(...funcArgs);
}

export async function runWasm(sendToTerminal: (buf: Uint8Array) => void, sendToPeer: (buf: Uint8Array) => void): Promise<SSHConnection> {
    const go = new Go();
    go.importObject.env = {
        sendToNetwork: (n: number) => {
            console.log(`Sending ${n} bytes to peer...`);
            sendToPeer(outgoingNetworkBuffer().slice(0, n));
        },
        sendToTerminal: (n: number) => {
            console.log('Sending to terminal...');
            sendToTerminal(outgoingTerminalBuffer().slice(0, n));
        }
    };
    const result = await WebAssembly.instantiateStreaming(fetch('main.wasm'), go.importObject);
    const inst = result.instance;

    go.run(inst);

    const exports = inst.exports as any;
    console.log(exports);
    const memory = exports.memory as WebAssembly.Memory;

    // Make these functions to evaluate at access time, as the underlying memory
    // buffer may change.
    const incomingNetworkBuffer = () => new Uint8Array(memory.buffer, exports.getIncomingNetworkBuffer(), BUFFER_SIZE_BYTES);
    const outgoingNetworkBuffer = () => new Uint8Array(memory.buffer, exports.getOutgoingNetworkBuffer(), BUFFER_SIZE_BYTES);
    const incomingTerminalBuffer = () => new Uint8Array(memory.buffer, exports.getIncomingTerminalBuffer(), BUFFER_SIZE_BYTES);
    const outgoingTerminalBuffer = () => new Uint8Array(memory.buffer, exports.getOutgoingTerminalBuffer(), BUFFER_SIZE_BYTES);

    const encoder = new TextEncoder();
    return {
        connect: (host: string, username: string, password: string) => {
            callFuncWithStrings(exports.connect, memory.buffer, exports.malloc, host, username, password);
        },
        readFromTerminal: (str: string) => {
            const buf = encoder.encode(str);
            incomingTerminalBuffer().set(buf);
            exports.receiveFromTerminal(buf.length);
        },
        readFromNetwork: (buf: Uint8Array) => {
            incomingNetworkBuffer().set(buf);
            exports.receiveFromNetwork(buf.length);
        },
    };
}

const sharedSecretField = document.getElementById('sharedSecret') as HTMLInputElement;
const usernameField = document.getElementById('username') as HTMLInputElement;
const passwordField = document.getElementById('password') as HTMLInputElement;
const connectButton = document.getElementById('connect') as HTMLButtonElement;

const term = new Terminal();
term.open(document.getElementById('terminal')!);

async function connectPeer(): Promise<ThingPeer> {
    const peerConfig = await createResponderConfig(sharedSecretField.value);
    const serverAuth = new InsecureServerAuth(peerConfig.pairingId, peerConfig.role);

    const listeners: Listeners = {
        connectionStateListener: async state => {
            console.log(`Peer connection state: ${state}`);
        }
    };

    const peer = new ThingPeer(SIGNALLING_SERVER_URL, serverAuth, peerConfig, listeners);
    peer.connect([]);

    return peer;
}

async function sshConnect(peer: ThingPeer) {
    const sshConnection = await runWasm(
        buf => term.write(buf),
        buf => dc.sendMessage(buf.buffer)
    );

    term.onData(str => {
        sshConnection.readFromTerminal(str);
    });

    const dc = await peer.createDataChannel(`tcp:${REMOTE_HOST}:22`, true);

    dc.on('binaryMessage', message => {
        console.log('Binary message received (JS)');
        sshConnection.readFromNetwork(new Uint8Array(message));
    });

    sshConnection.connect(`${REMOTE_HOST}:22`, usernameField.value, passwordField.value);
}

connectButton.addEventListener('click', async () => {
    console.log('Connecting...');
    const peer = await connectPeer();
    await sshConnect(peer);
});
