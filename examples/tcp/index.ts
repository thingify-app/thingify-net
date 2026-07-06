import { createResponderConfig, InsecureServerAuth, Listeners, ThingPeer } from 'thingrtc-peer';

const SIGNALLING_SERVER_URL = 'wss://thingify.deno.dev/signalling';

const sharedSecretField = document.getElementById('sharedSecret') as HTMLInputElement;
const connectButton = document.getElementById('connect') as HTMLButtonElement;

const remoteAddressField = document.getElementById('remoteAddress') as HTMLInputElement;
const createTcpButton = document.getElementById('createTcp') as HTMLButtonElement;
const createUdpButton = document.getElementById('createUdp') as HTMLButtonElement;

const connectionsDiv = document.getElementById('connections') as HTMLDivElement;

let peer: ThingPeer;

connectButton.addEventListener('click', async () => {
    console.log('Connecting...');

    const listeners: Listeners = {
        connectionStateListener: async state => {
            console.log(`Peer connection state: ${state}`);
        }
    };

    const peerConfig = await createResponderConfig(sharedSecretField.value);
    const serverAuth = new InsecureServerAuth(peerConfig.pairingId, peerConfig.role);

    peer = new ThingPeer(SIGNALLING_SERVER_URL, serverAuth, peerConfig, listeners);
    peer.connect([]);
});

createTcpButton.addEventListener('click', async () => {
    await createConnection('tcp');
});

createUdpButton.addEventListener('click', async () => {
    await createConnection('udp');
});

async function createConnection(protocol: 'tcp'|'udp'): Promise<void> {
    const remoteAddress = remoteAddressField.value;
    const reliable = protocol === 'tcp';
    const dc = await peer.createDataChannel(`${protocol}:${remoteAddress}`, reliable);
    
    const rowContainer = document.createElement('div');

    const statusField = document.createElement('span');
    statusField.innerText = `${remoteAddress} (${protocol}) connected: `;
    rowContainer.appendChild(statusField);
    
    const messageField = document.createElement('input');
    messageField.type = 'text';
    messageField.placeholder = 'Message';
    rowContainer.appendChild(messageField);

    const sendButton = document.createElement('button');
    sendButton.textContent = 'Send';
    rowContainer.appendChild(sendButton);

    connectionsDiv.appendChild(rowContainer);

    dc.on('stringMessage', message => {
        console.log(`Message received: ${message}`);
    });

    dc.on('binaryMessage', message => {
        console.log('Binary message received:');
        console.log(message);
    });

    dc.on('close', () => {
        console.log('Data channel closed.');
        statusField.innerText = `${remoteAddress} closed.`;
        rowContainer.removeChild(messageField);
        rowContainer.removeChild(sendButton);
    });

    sendButton.addEventListener('click', async () => {
        const message = messageField.value;
        await dc.sendMessage(message);
    });
}
