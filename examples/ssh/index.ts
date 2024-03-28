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

runWasm();
