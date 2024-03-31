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

const PAIRING_SERVER_URL = 'https://thingify.deno.dev/pairing';
const SIGNALLING_SERVER_URL = 'wss://thingify.deno.dev/signalling';
const pairingServer = new PairingServer(PAIRING_SERVER_URL);
const pairing = new Pairing(pairingServer);

const peer = new ThingPeer(SIGNALLING_SERVER_URL);
peer.on('connectionStateChanged', state => {
    console.log(`Peer connection state: ${state}`);
});
peer.on('binaryMessage', message => {
    console.log('Binary message received:');
    console.log(message);
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
    await connect();
    await runWasm();
}

main();
