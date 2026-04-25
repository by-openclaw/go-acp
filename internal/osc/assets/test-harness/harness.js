#!/usr/bin/env node
// OSC reference peer for dhs codec validation.
//
// Modes:
//
//   encode       : JSON spec (stdin or --spec FILE) -> OSC bytes (stdout / --out FILE / --hex)
//   decode       : OSC bytes (stdin / --in FILE / --hex) -> JSON spec (stdout)
//   send-udp     : read JSON spec, send via UDP to --host:--port
//   send-tcp-len : read JSON spec, send via TCP with int32-BE length prefix to --host:--port
//   send-tcp-slip: read JSON spec, send via TCP with SLIP framing to --host:--port
//   listen-udp     : bind --port (and optionally --host), print decoded JSON for each datagram
//   listen-tcp-len : bind --port for TCP length-prefix, print decoded JSON for each frame
//   listen-tcp-slip: bind --port for TCP SLIP, print decoded JSON for each frame
//
// All modes read OSC packets as the osc.js "metadata: true" form so every
// type tag (including OSC 1.1 [ ] array markers) round-trips.

'use strict';

const osc  = require('osc');
const fs   = require('fs');
const net  = require('net');
const dgram= require('dgram');

const [, , mode, ...rawArgs] = process.argv;

const args = Object.fromEntries(rawArgs.reduce((acc, tok, i, all) => {
    if (tok.startsWith('--')) acc.push([tok.slice(2), all[i + 1] ?? true]);
    return acc;
}, []));

const isStdinSrc = (s) => s === undefined || s === true || s === '' || s === '-';

const readSource = () => {
    const key = 'spec' in args ? 'spec' : 'in';
    const src = args[key];
    return isStdinSrc(src) ? fs.readFileSync(0) : fs.readFileSync(String(src));
};

const writeEncoded = (buf) => {
    if (args.out) fs.writeFileSync(String(args.out), buf);
    else if (args.hex) process.stdout.write(buf.toString('hex') + '\n');
    else process.stdout.write(buf);
};

const replacer = (_k, v) => (typeof v === 'bigint' ? v.toString() : v);

const SLIP_END = 0xC0;
const SLIP_ESC = 0xDB;

function slipFrame(buf) {
    const out = [SLIP_END];
    for (const b of buf) {
        if (b === SLIP_END) { out.push(SLIP_ESC, 0xDC); }
        else if (b === SLIP_ESC) { out.push(SLIP_ESC, 0xDD); }
        else { out.push(b); }
    }
    out.push(SLIP_END);
    return Buffer.from(out);
}

// slipUnstuff returns array of {payload: Buffer, remainder: Buffer}.
function slipUnstuffStream(state, chunk) {
    state.buf = Buffer.concat([state.buf, chunk]);
    const frames = [];
    let i = 0;
    let frameStart = -1;
    while (i < state.buf.length) {
        const b = state.buf[i];
        if (b === SLIP_END) {
            if (frameStart >= 0) {
                // close frame
                const raw = state.buf.slice(frameStart, i);
                if (raw.length > 0) {
                    frames.push(unescapeSlip(raw));
                }
                frameStart = -1;
            } else {
                // start frame
                frameStart = i + 1;
            }
        }
        i++;
    }
    // Keep unconsumed remainder for next chunk.
    if (frameStart >= 0) {
        state.buf = state.buf.slice(frameStart - 1); // include the leading END
    } else {
        state.buf = Buffer.alloc(0);
    }
    return frames;
}

function unescapeSlip(buf) {
    const out = [];
    for (let i = 0; i < buf.length; i++) {
        const b = buf[i];
        if (b === SLIP_ESC) {
            const nxt = buf[i + 1];
            if (nxt === 0xDC) out.push(SLIP_END);
            else if (nxt === 0xDD) out.push(SLIP_ESC);
            else throw new Error(`bad SLIP escape: ${nxt}`);
            i++;
        } else {
            out.push(b);
        }
    }
    return Buffer.from(out);
}

function readLengthPrefix(state, chunk) {
    state.buf = Buffer.concat([state.buf, chunk]);
    const frames = [];
    while (state.buf.length >= 4) {
        const sz = state.buf.readInt32BE(0);
        if (state.buf.length < 4 + sz) break;
        frames.push(state.buf.slice(4, 4 + sz));
        state.buf = state.buf.slice(4 + sz);
    }
    return frames;
}

function decodeAndPrint(buf) {
    try {
        const spec = osc.readPacket(buf, { metadata: true });
        process.stdout.write(JSON.stringify(spec, replacer) + '\n');
    } catch (e) {
        process.stderr.write(`decode error: ${e.message}\n`);
    }
}

switch (mode) {
    case 'encode': {
        const spec = JSON.parse(readSource().toString('utf8'));
        const buf = Buffer.from(osc.writePacket(spec, { metadata: true }));
        writeEncoded(buf);
        break;
    }
    case 'decode': {
        let buf = readSource();
        if (args.hex) buf = Buffer.from(buf.toString('utf8').trim(), 'hex');
        const spec = osc.readPacket(buf, { metadata: true });
        process.stdout.write(JSON.stringify(spec, replacer, 2) + '\n');
        break;
    }
    case 'send-udp': {
        const spec = JSON.parse(readSource().toString('utf8'));
        const buf = Buffer.from(osc.writePacket(spec, { metadata: true }));
        const sock = dgram.createSocket('udp4');
        sock.send(buf, parseInt(args.port, 10), args.host || '127.0.0.1', (err) => {
            sock.close();
            if (err) { process.stderr.write(`send-udp: ${err}\n`); process.exit(1); }
        });
        break;
    }
    case 'send-tcp-len':
    case 'send-tcp-slip': {
        const spec = JSON.parse(readSource().toString('utf8'));
        const buf = Buffer.from(osc.writePacket(spec, { metadata: true }));
        const framed = mode === 'send-tcp-slip'
            ? slipFrame(buf)
            : Buffer.concat([Buffer.from([(buf.length >>> 24) & 0xFF, (buf.length >>> 16) & 0xFF, (buf.length >>> 8) & 0xFF, buf.length & 0xFF]), buf]);
        const sock = net.connect({ host: args.host || '127.0.0.1', port: parseInt(args.port, 10) }, () => {
            sock.write(framed, () => sock.end());
        });
        sock.on('error', (err) => { process.stderr.write(`send: ${err}\n`); process.exit(1); });
        break;
    }
    case 'listen-udp': {
        const sock = dgram.createSocket('udp4');
        sock.on('message', (buf) => decodeAndPrint(buf));
        sock.bind(parseInt(args.port, 10), args.host || '127.0.0.1', () => {
            const a = sock.address();
            process.stderr.write(`listen-udp ready on ${a.address}:${a.port}\n`);
        });
        break;
    }
    case 'listen-tcp-len':
    case 'listen-tcp-slip': {
        const srv = net.createServer((conn) => {
            const state = { buf: Buffer.alloc(0) };
            conn.on('data', (chunk) => {
                const frames = mode === 'listen-tcp-slip'
                    ? slipUnstuffStream(state, chunk)
                    : readLengthPrefix(state, chunk);
                for (const f of frames) decodeAndPrint(f);
            });
            conn.on('error', () => {});
        });
        srv.listen(parseInt(args.port, 10), args.host || '127.0.0.1', () => {
            const a = srv.address();
            process.stderr.write(`${mode} ready on ${a.address}:${a.port}\n`);
        });
        break;
    }
    default:
        process.stderr.write('usage: harness.js {encode|decode|send-udp|send-tcp-len|send-tcp-slip|listen-udp|listen-tcp-len|listen-tcp-slip} [--host H] [--port P] [--spec|--in FILE] [--out FILE] [--hex]\n');
        process.exit(1);
}
