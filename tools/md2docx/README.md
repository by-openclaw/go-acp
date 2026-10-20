# md2docx

A Markdown note to `.docx` with the layout of the RTBF statement: A4 landscape, 2 cm margins, Arial, blue table headers, status cells coloured, footer with the page number.

```sh
cd tools/md2docx && npm ci
node md2docx.js ../../internal/rcp/docs/<note>.md ../../internal/rcp/docs/<note>.docx "<footer label>"
```

The `.md` is the source; the `.docx` is a generated annex, not committed.
