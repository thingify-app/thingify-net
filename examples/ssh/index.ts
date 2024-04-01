import { Pairing, PairingServer, ThingPeer } from 'thingrtc-peer';
import { Terminal } from 'xterm';

export async function runWasm() {
    const go = new Go();
    const result = await WebAssembly.instantiateStreaming(fetch('main.wasm'), go.importObject);
    const inst = result.instance;
    await go.run(inst);
}

const term = new Terminal();
term.open(document.getElementById('terminal')!);
term.write('Hello from \x1B[1;3;31mxterm.js\x1B[0m $ ')
term.onData(str => {
    console.log(str);
});

globalThis.writeToConsole = (str: string) => {
    term.write(str);
}

// Create a global buffer for WASM to write to, before calling sendToPeer
// with the actual number of bytes to send from the buffer.
const messageBuffer = new Uint8Array(2048);
globalThis.messageBuffer = messageBuffer;
globalThis.sendToPeer = (len: number) => {
    const buffer = messageBuffer.subarray(0, len);
    console.log(`Sending message ${buffer}`);
    peer.sendMessage(buffer);
}

const PAIRING_SERVER_URL = 'https://thingify.deno.dev/pairing';
const SIGNALLING_SERVER_URL = 'wss://thingify.deno.dev/signalling';
const pairingServer = new PairingServer(PAIRING_SERVER_URL);
const pairing = new Pairing(pairingServer);

const peer = new ThingPeer(SIGNALLING_SERVER_URL);
peer.on('connectionStateChanged', state => {
    console.log(`Peer connection state: ${state}`);
});

// Create a global buffer for WASM to read from, before calling its
// messageListener function to read the buffer.
const outgoingMessageBuffer = new Uint8Array(2048);
globalThis.outgoingMessageBuffer = outgoingMessageBuffer;
peer.on('binaryMessage', message => {
    console.log('Binary message received (JS)');
    outgoingMessageBuffer.set(new Uint8Array(message), 0);
    if (globalThis.messageListener) {
        globalThis.messageListener(message.byteLength);
    }
});

async function tryPairing() {
    const pendingPairing = await pairing.initiatePairing();
    const shortcode = pendingPairing.shortcode;
    console.log(`Initiating pairing, shortcode: ${shortcode}`);
    await pendingPairing.redemptionResult();
}

async function connect() {
    let pairingIds: string[] = [];
    while (true) {
        pairingIds = await pairing.getAllPairingIds();
        if (pairingIds.length == 0) {
            await tryPairing();
        } else {
            break;
        }
    }

    const tokenGenerator = await pairing.getTokenGenerator(pairingIds[0]);
    peer.connect(tokenGenerator, []);
}

async function clearPairings() {
    peer.disconnect();
    await pairing.clearAllPairings();
    await connect();
}

async function main() {
    document.getElementById('clearPairings').addEventListener('click', () => {
        clearPairings();
    });
    console.log('Connecting...');
    await connect();
    console.log('Running wasm...');
    await runWasm();
}

main();
