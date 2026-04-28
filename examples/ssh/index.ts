import { createResponderConfig, InsecureServerAuth, ThingPeer } from 'thingrtc-peer';
import { Terminal } from 'xterm';

// Declare functions/variables accessible to WASM.
declare global {
    function init(remoteHost: string, username: string, password: string): void;
    function stdInListener(term: string): void;
    function messageListener(len: number): void;
    function writeToConsole(str: string): void;
    function sendToPeer(len: number): void;
    var messageBuffer: Uint8Array;
    var outgoingMessageBuffer: Uint8Array;
}

const SIGNALLING_SERVER_URL = 'wss://thingify.deno.dev/signalling';
const REMOTE_HOST = '10.0.1.1';
const MTU_BYTES = 1024;

export async function runWasm() {
    const go = new Go();
    const result = await WebAssembly.instantiateStreaming(fetch('main.wasm'), go.importObject);
    const inst = result.instance;
    await go.run(inst);
}

const sharedSecretField = document.getElementById('sharedSecret') as HTMLInputElement;
const usernameField = document.getElementById('username') as HTMLInputElement;
const passwordField = document.getElementById('password') as HTMLInputElement;
const connectButton = document.getElementById('connect') as HTMLButtonElement;
const sshConnectButton = document.getElementById('sshConnect') as HTMLButtonElement;

const term = new Terminal();
term.open(document.getElementById('terminal')!);
term.onData(str => {
    if (globalThis.stdInListener) {
        globalThis.stdInListener(str);
    }
});

globalThis.writeToConsole = (str: string) => {
    term.write(str);
}

// Create a global buffer for WASM to write to, before calling sendToPeer
// with the actual number of bytes to send from the buffer.
const messageBuffer = new Uint8Array(MTU_BYTES);
globalThis.messageBuffer = messageBuffer;

// Create a global buffer for WASM to read from, before calling its
// messageListener function to read the buffer.
const outgoingMessageBuffer = new Uint8Array(MTU_BYTES);
globalThis.outgoingMessageBuffer = outgoingMessageBuffer;

async function connect() {
    const peerConfig = await createResponderConfig(sharedSecretField.value);
    const serverAuth = new InsecureServerAuth(peerConfig.pairingId, peerConfig.role);
    const peer = new ThingPeer(SIGNALLING_SERVER_URL, serverAuth, peerConfig);

    globalThis.sendToPeer = (len: number) => {
        const buffer = messageBuffer.subarray(0, len).buffer;
        console.log(`Sending message ${buffer}`);
        peer.sendMessage(buffer);
    }

    peer.on('connectionStateChanged', state => {
        console.log(`Peer connection state: ${state}`);
    });

    peer.on('binaryMessage', message => {
        console.log('Binary message received (JS)');
        outgoingMessageBuffer.set(new Uint8Array(message), 0);
        if (globalThis.messageListener) {
            globalThis.messageListener(message.byteLength);
        }
    });

    peer.connect([]);
}

async function startInit() {
    console.log('Initing...');
    globalThis.init(REMOTE_HOST, usernameField.value, passwordField.value);
}

async function main() {
    connectButton.addEventListener('click', async () => {
        console.log('Connecting...');
        await connect();
    });

    sshConnectButton.addEventListener('click', async () => {
        await startInit();
    });

    console.log('Running wasm...');
    await runWasm();
}

main();
