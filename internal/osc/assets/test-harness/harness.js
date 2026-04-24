#!/usr/bin/env node
// OSC reference peer for dhs codec validation.
//
// encode: JSON spec (stdin or --spec FILE) -> OSC bytes (stdout / --out FILE / --hex)
// decode: OSC bytes (stdin / --in FILE / --hex) -> JSON spec (stdout)
//
// Spec shape, metadata form (osc.js "metadata: true"):
//   message: { address: "/path", args: [{ type: "i", value: 42 }, ...] }
//   bundle : { timeTag: { raw: [secs, frac] }, packets: [msg-or-bundle, ...] }
//
// Supports every type osc.js encodes, including OSC 1.1 array markers via
// { type: "[" } ... { type: "]" } sentinel args.

'use strict';

const osc = require('osc');
const fs  = require('fs');

const [, , mode, ...rawArgs] = process.argv;

const args = Object.fromEntries(rawArgs.reduce((acc, tok, i, all) => {
    if (tok.startsWith('--')) acc.push([tok.slice(2), all[i + 1] ?? true]);
    return acc;
}, []));

const readSource = () => {
    const key = 'spec' in args ? 'spec' : 'in';
    const src = args[key];
    return src && src !== true ? fs.readFileSync(String(src)) : fs.readFileSync(0);
};

const writeEncoded = (buf) => {
    if (args.out) fs.writeFileSync(String(args.out), buf);
    else if (args.hex) process.stdout.write(buf.toString('hex') + '\n');
    else process.stdout.write(buf);
};

const replacer = (_k, v) => (typeof v === 'bigint' ? v.toString() : v);

if (mode === 'encode') {
    const spec = JSON.parse(readSource().toString('utf8'));
    const buf = Buffer.from(osc.writePacket(spec, { metadata: true }));
    writeEncoded(buf);
} else if (mode === 'decode') {
    let buf = readSource();
    if (args.hex) buf = Buffer.from(buf.toString('utf8').trim(), 'hex');
    const spec = osc.readPacket(buf, { metadata: true });
    process.stdout.write(JSON.stringify(spec, replacer, 2) + '\n');
} else {
    process.stderr.write('usage: harness.js {encode|decode} [--spec|--in FILE] [--out FILE] [--hex]\n');
    process.exit(1);
}
