// md2docx — a Markdown note to .docx with the layout of the RTBF statement
// "Cerebrum — RCP and Northbound API: which one for which feature":
// A4 landscape, 2 cm margins, Arial; a bold title, a From / Date / Status /
// Owner block, a "Contents" field over headings 1-2, numbered headings,
// tables with a blue header row and status cells coloured by their first
// words (proven = green, document / accepted = yellow, no / 404 = red),
// a footer "<label> — page N".
//
//   node md2docx.js <in.md> <out.docx> "<footer label>"
const fs = require("fs");
const path = require("path");
const {
  Document, Packer, Paragraph, TextRun, Table, TableRow, TableCell, WidthType,
  AlignmentType, Footer, PageNumber, HeadingLevel, ShadingType, BorderStyle,
  PageOrientation, LevelFormat, TableOfContents,
} = require("docx");

const [, , inFile, outFile, footerLabel = "RTBF — Cerebrum RCP and Northbound"] = process.argv;
if (!inFile || !outFile) { console.error("usage: node md2docx.js in.md out.docx [footer label]"); process.exit(2); }
const md = fs.readFileSync(inFile, "utf8").replace(/\r\n/g, "\n");

const FONT = "Arial";
const SZ = { title: 40, meta: 20, contents: 26, h1: 28, h2: 24, h3: 22, body: 20, table: 18, footer: 16 };
const FILL = { head: "DCE6F1", green: "E2F0D9", yellow: "FFF2CC", red: "F8D7DA" };
const BORDER = { style: BorderStyle.SINGLE, size: 4, color: "BFBFBF" };
const BORDERS = { top: BORDER, bottom: BORDER, left: BORDER, right: BORDER };
const PAGE_W = 16838, MARGIN = 1134, TEXT_W = PAGE_W - 2 * MARGIN; // 14570 dxa

// Inline markdown → runs: **bold**, `code`, *em*. Code keeps the body font
// (the statement uses Arial only).
function runs(text, opts = {}) {
  const out = [];
  const re = /(\*\*[^*]+\*\*|`[^`]+`|\*[^*]+\*)/g;
  let last = 0, m;
  const push = (t, extra = {}) => out.push(new TextRun({ text: t, font: FONT, size: opts.size, bold: opts.bold, ...extra }));
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) push(text.slice(last, m.index));
    const t = m[0];
    if (t.startsWith("**")) push(t.slice(2, -2), { bold: true });
    else if (t.startsWith("`")) push(t.slice(1, -1));
    else push(t.slice(1, -1), { italics: true });
    last = m.index + t.length;
  }
  if (last < text.length) push(text.slice(last));
  return out.length ? out : [new TextRun({ text: "", font: FONT, size: opts.size })];
}

// Status colour of a table cell from its first words (the legend of §4).
function fillFor(text) {
  const t = text.trim().toLowerCase().replace(/[*`]/g, "");
  if (/^proven\b/.test(t)) return FILL.green;
  if (/^(no\b|not writable|not applicable|not on (the )?server|404\b|400\b|refused|open\b|missing\b)/.test(t)) return FILL.red;
  if (/^(document\b|back-channel document|accepted\b|partly\b|written to the document|tested\b|stored as written|only (when|for)|list answered|request form|subscription accepted|not tried|available;|acknowledged|202\b|unclear|pending|manual\b|to measure)/.test(t)) return FILL.yellow;
  return undefined;
}

function cell(text, { head = false, align, width } = {}) {
  const fill = head ? FILL.head : fillFor(text);
  return new TableCell({
    borders: BORDERS,
    width: width ? { size: width, type: WidthType.DXA } : undefined,
    shading: fill ? { type: ShadingType.CLEAR, fill } : undefined,
    margins: { top: 60, bottom: 60, left: 100, right: 100 },
    children: [new Paragraph({ alignment: align, children: runs(text, { size: SZ.table, bold: head }) })],
  });
}

// Column widths: the statement's six-column feature table, else a narrow
// "#" column and the rest shared evenly.
function widths(header) {
  const n = header.length;
  if (n === 6 && header[0] === "#") return [381, 4078, 4077, 1848, 3065, 1121];
  if (header[0] === "#" || header[0] === "Mark") {
    const first = header[0] === "#" ? 381 : 1400;
    const rest = Math.floor((TEXT_W - first) / (n - 1));
    return [first, ...Array(n - 1).fill(rest)];
  }
  return Array(n).fill(Math.floor(TEXT_W / n));
}

function table(lines) {
  // A cell may carry an escaped pipe (`\|`, the Markdown way of writing "|" inside a table).
  const rows = lines.filter((l) => !/^\|\s*:?-+/.test(l)).map((l) => l.replace(/^\||\|$/g, "").split(/(?<!\\)\|/).map((c) => c.trim().replace(/\\\|/g, "|")));
  const aligns = (lines.find((l) => /^\|\s*:?-+/.test(l)) || "").replace(/^\||\|$/g, "").split("|").map((c) => (c.trim().endsWith(":") ? AlignmentType.RIGHT : undefined));
  const ncol = Math.max(...rows.map((r) => r.length));
  const w = widths(rows[0]);
  return new Table({
    width: { size: TEXT_W, type: WidthType.DXA },
    columnWidths: w,
    rows: rows.map((r, i) => new TableRow({
      tableHeader: i === 0,
      children: Array.from({ length: ncol }, (_, j) => cell(r[j] || "", { head: i === 0, align: aligns[j], width: w[j] })),
    })),
  });
}

const children = [];
const lines = md.split("\n");
let i = 0, para = [], sawTitle = false, metaDone = false;
const flushPara = () => { if (para.length) { children.push(new Paragraph({ spacing: { after: 120 }, children: runs(para.join(" "), { size: SZ.body }) })); para = []; } };
const contents = () => {
  children.push(new Paragraph({ spacing: { before: 200, after: 120 }, children: [new TextRun({ text: "Contents", font: FONT, size: SZ.contents, bold: true })] }));
  children.push(new TableOfContents("Contents", { hyperlink: true, headingStyleRange: "1-2" }));
  children.push(new Paragraph({ spacing: { after: 120 }, children: [] }));
  metaDone = true;
};
while (i < lines.length) {
  const l = lines[i];
  if (/^#\s/.test(l)) { flushPara(); children.push(new Paragraph({ spacing: { after: 200 }, children: [new TextRun({ text: l.slice(2), font: FONT, size: SZ.title, bold: true })] })); sawTitle = true; i++; continue; }
  if (/^##\s/.test(l)) { flushPara(); if (sawTitle && !metaDone) contents(); children.push(new Paragraph({ heading: HeadingLevel.HEADING_1, spacing: { before: 280, after: 120 }, children: runs(l.slice(3), { size: SZ.h1, bold: true }) })); i++; continue; }
  if (/^###\s/.test(l)) { flushPara(); children.push(new Paragraph({ heading: HeadingLevel.HEADING_2, spacing: { before: 200, after: 100 }, children: runs(l.slice(4), { size: SZ.h2, bold: true }) })); i++; continue; }
  if (/^\|/.test(l)) { flushPara(); const t = []; while (i < lines.length && /^\|/.test(lines[i])) t.push(lines[i++]); children.push(table(t)); children.push(new Paragraph({ spacing: { after: 120 }, children: [] })); continue; }
  if (/^[-*]\s/.test(l)) { flushPara(); while (i < lines.length && /^[-*]\s/.test(lines[i])) { children.push(new Paragraph({ numbering: { reference: "bullets", level: 0 }, spacing: { after: 60 }, children: runs(lines[i].slice(2), { size: SZ.body }) })); i++; } continue; }
  if (/^\d+\.\s/.test(l)) { flushPara(); while (i < lines.length && /^\d+\.\s/.test(lines[i])) { children.push(new Paragraph({ numbering: { reference: "numbers", level: 0 }, spacing: { after: 60 }, children: runs(lines[i].replace(/^\d+\.\s/, ""), { size: SZ.body }) })); i++; } continue; }
  if (/^\*\*[^*]+:\*\*/.test(l) && !metaDone) { flushPara(); children.push(new Paragraph({ spacing: { after: 40 }, children: runs(l, { size: SZ.meta }) })); i++; continue; }
  if (l.trim() === "") { flushPara(); i++; continue; }
  para.push(l.trim()); i++;
}
flushPara();

const doc = new Document({
  features: { updateFields: true },
  styles: { default: { document: { run: { font: FONT, size: SZ.body } } } },
  numbering: {
    config: [
      { reference: "bullets", levels: [{ level: 0, format: LevelFormat.BULLET, text: "•", alignment: AlignmentType.LEFT, style: { paragraph: { indent: { left: 560, hanging: 280 } } } }] },
      { reference: "numbers", levels: [{ level: 0, format: LevelFormat.DECIMAL, text: "%1.", alignment: AlignmentType.LEFT, style: { paragraph: { indent: { left: 560, hanging: 280 } } } }] },
    ],
  },
  sections: [{
    // docx swaps width and height itself for LANDSCAPE: pass the portrait A4 values.
    properties: { page: { size: { orientation: PageOrientation.LANDSCAPE, width: 11906, height: 16838 }, margin: { top: MARGIN, right: MARGIN, bottom: MARGIN, left: MARGIN, header: 708, footer: 708 } } },
    footers: { default: new Footer({ children: [new Paragraph({ alignment: AlignmentType.CENTER, children: [new TextRun({ text: `${footerLabel} — page `, font: FONT, size: SZ.footer }), new TextRun({ children: [PageNumber.CURRENT], font: FONT, size: SZ.footer })] })] }) },
    children,
  }],
});

Packer.toBuffer(doc).then((buf) => { fs.writeFileSync(outFile, buf); console.log(`${path.basename(outFile)} ${buf.length} bytes`); });
