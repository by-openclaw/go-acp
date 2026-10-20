// md2docx — a Markdown note to .docx with the layout of the RTBF statement:
// A4 landscape, 2 cm margins, Arial, blue table headers, status cells
// coloured (green / yellow / red), footer "<label> — page N".
//
//   node md2docx.js <in.md> <out.docx> "<footer label>"
const fs = require("fs");
const path = require("path");
const {
  Document, Packer, Paragraph, TextRun, Table, TableRow, TableCell, WidthType,
  AlignmentType, Footer, PageNumber, HeadingLevel, ShadingType, BorderStyle,
  PageOrientation, LevelFormat,
} = require("docx");

const [, , inFile, outFile, footerLabel = "BY-SYSTEMS — Cerebrum RCP"] = process.argv;
if (!inFile || !outFile) { console.error("usage: node md2docx.js in.md out.docx [footer label]"); process.exit(2); }
const md = fs.readFileSync(inFile, "utf8").replace(/\r\n/g, "\n");

const FONT = "Arial";
const SZ = { title: 56, h1: 28, h2: 24, h3: 22, body: 20, table: 18 };
const FILL = { head: "DCE6F1", green: "E2F0D9", yellow: "FFF2CC", red: "F8D7DA" };
const BORDER = { style: BorderStyle.SINGLE, size: 4, color: "BFBFBF" };
const BORDERS = { top: BORDER, bottom: BORDER, left: BORDER, right: BORDER };

// Inline markdown → runs: **bold**, `code`, *em*.
function runs(text, opts = {}) {
  const out = [];
  const re = /(\*\*[^*]+\*\*|`[^`]+`|\*[^*]+\*)/g;
  let last = 0, m;
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) out.push(new TextRun({ text: text.slice(last, m.index), font: FONT, size: opts.size, bold: opts.bold }));
    const t = m[0];
    if (t.startsWith("**")) out.push(new TextRun({ text: t.slice(2, -2), font: FONT, size: opts.size, bold: true }));
    else if (t.startsWith("`")) out.push(new TextRun({ text: t.slice(1, -1), font: "Consolas", size: opts.size, shading: { type: ShadingType.CLEAR, fill: "F2F2F2" } }));
    else out.push(new TextRun({ text: t.slice(1, -1), font: FONT, size: opts.size, italics: true, bold: opts.bold }));
    last = m.index + t.length;
  }
  if (last < text.length) out.push(new TextRun({ text: text.slice(last), font: FONT, size: opts.size, bold: opts.bold }));
  return out.length ? out : [new TextRun({ text: "", font: FONT, size: opts.size })];
}

// Status colour of a table cell from its own text.
function fillFor(text) {
  const t = text.trim().toLowerCase().replace(/[*`]/g, "");
  if (/^(ok|yes|works|pass(ed)?|applied|done|green)\b/.test(t)) return FILL.green;
  if (/^(no|refused|not (on|available|applied|supported)|absent|fail|red|open|unchanged)\b/.test(t) || /^4\d\d\b/.test(t)) return FILL.red;
  if (/^(partial|accepted|pending|unclear|manual|yellow|n\/a)\b/.test(t) || /^202\b/.test(t)) return FILL.yellow;
  return undefined;
}

function cell(text, { head = false, align } = {}) {
  const fill = head ? FILL.head : fillFor(text);
  return new TableCell({
    borders: BORDERS,
    shading: fill ? { type: ShadingType.CLEAR, fill } : undefined,
    margins: { top: 60, bottom: 60, left: 100, right: 100 },
    children: [new Paragraph({ alignment: align, children: runs(text, { size: SZ.table, bold: head }) })],
  });
}

function table(lines) {
  const rows = lines.filter((l) => !/^\|\s*:?-+/.test(l)).map((l) => l.replace(/^\||\|$/g, "").split("|").map((c) => c.trim()));
  const aligns = (lines.find((l) => /^\|\s*:?-+/.test(l)) || "").replace(/^\||\|$/g, "").split("|").map((c) => (c.trim().endsWith(":") ? AlignmentType.RIGHT : undefined));
  const ncol = Math.max(...rows.map((r) => r.length));
  return new Table({
    width: { size: 100, type: WidthType.PERCENTAGE },
    rows: rows.map((r, i) => new TableRow({
      tableHeader: i === 0,
      children: Array.from({ length: ncol }, (_, j) => cell(r[j] || "", { head: i === 0, align: aligns[j] })),
    })),
  });
}

const children = [];
const lines = md.split("\n");
let i = 0, para = [];
const flushPara = () => { if (para.length) { children.push(new Paragraph({ spacing: { after: 120 }, children: runs(para.join(" "), { size: SZ.body }) })); para = []; } };
while (i < lines.length) {
  const l = lines[i];
  if (/^#\s/.test(l)) { flushPara(); children.push(new Paragraph({ heading: HeadingLevel.TITLE, spacing: { after: 240 }, children: runs(l.slice(2), { size: SZ.title, bold: true }) })); i++; continue; }
  if (/^##\s/.test(l)) { flushPara(); children.push(new Paragraph({ heading: HeadingLevel.HEADING_1, spacing: { before: 280, after: 120 }, children: runs(l.slice(3), { size: SZ.h1, bold: true }) })); i++; continue; }
  if (/^###\s/.test(l)) { flushPara(); children.push(new Paragraph({ heading: HeadingLevel.HEADING_2, spacing: { before: 200, after: 100 }, children: runs(l.slice(4), { size: SZ.h2, bold: true }) })); i++; continue; }
  if (/^\|/.test(l)) { flushPara(); const t = []; while (i < lines.length && /^\|/.test(lines[i])) t.push(lines[i++]); children.push(table(t)); children.push(new Paragraph({ spacing: { after: 120 }, children: [] })); continue; }
  if (/^[-*]\s/.test(l)) { flushPara(); while (i < lines.length && /^[-*]\s/.test(lines[i])) { children.push(new Paragraph({ numbering: { reference: "bullets", level: 0 }, spacing: { after: 60 }, children: runs(lines[i].slice(2), { size: SZ.body }) })); i++; } continue; }
  if (/^\d+\.\s/.test(l)) { flushPara(); while (i < lines.length && /^\d+\.\s/.test(lines[i])) { children.push(new Paragraph({ numbering: { reference: "numbers", level: 0 }, spacing: { after: 60 }, children: runs(lines[i].replace(/^\d+\.\s/, ""), { size: SZ.body }) })); i++; } continue; }
  if (/^\*\*[^*]+:\*\*/.test(l)) { flushPara(); children.push(new Paragraph({ spacing: { after: 40 }, children: runs(l, { size: SZ.body }) })); i++; continue; }
  if (l.trim() === "") { flushPara(); i++; continue; }
  para.push(l.trim()); i++;
}
flushPara();

const doc = new Document({
  styles: { default: { document: { run: { font: FONT, size: SZ.body } } } },
  numbering: {
    config: [
      { reference: "bullets", levels: [{ level: 0, format: LevelFormat.BULLET, text: "•", alignment: AlignmentType.LEFT, style: { paragraph: { indent: { left: 560, hanging: 280 } } } }] },
      { reference: "numbers", levels: [{ level: 0, format: LevelFormat.DECIMAL, text: "%1.", alignment: AlignmentType.LEFT, style: { paragraph: { indent: { left: 560, hanging: 280 } } } }] },
    ],
  },
  sections: [{
    // docx swaps width and height itself for LANDSCAPE: pass the portrait A4 values.
    properties: { page: { size: { orientation: PageOrientation.LANDSCAPE, width: 11906, height: 16838 }, margin: { top: 1134, right: 1134, bottom: 1134, left: 1134, header: 708, footer: 708 } } },
    footers: { default: new Footer({ children: [new Paragraph({ alignment: AlignmentType.CENTER, children: [new TextRun({ text: `${footerLabel} — page `, font: FONT, size: 16 }), new TextRun({ children: [PageNumber.CURRENT], font: FONT, size: 16 })] })] }) },
    children,
  }],
});

Packer.toBuffer(doc).then((buf) => { fs.writeFileSync(outFile, buf); console.log(`${path.basename(outFile)} ${buf.length} bytes`); });
