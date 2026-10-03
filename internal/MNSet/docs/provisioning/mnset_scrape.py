"""Scrape MN SET's rendered views of one device, READ ONLY.

Clicks only: the device row, tab labels, and Material drop-downs (opened to read
their options, closed with Escape). Never Apply / Save / Update / Reset.

usage: python mnset_scrape.py <scratch-dir> <.secrets/mnset.json> <device-name> <out.csv>
"""
import csv, json, sys
from playwright.sync_api import sync_playwright

SP, SEC, DEV, OUT = sys.argv[1:5]
sec = json.load(open(SEC))["fields"]
URL = f"http://{sec['host']}:{sec['rest_port']}/"

JS = r"""(root) => {
  const vis = e => !!(e.offsetWidth || e.offsetHeight || e.getClientRects().length);
  const txt = e => (e ? (e.innerText || e.textContent || '').replace(/\s+/g, ' ').trim() : '');
  const labelOf = el => {
    if (el.id) { const l = root.querySelector('label[for="' + el.id + '"]'); if (l && txt(l)) return txt(l); }
    let n = el;
    for (let i = 0; i < 6 && n; i++) {
      n = n.parentElement; if (!n) break;
      const l = n.querySelector('label, .col-form-label, th');
      if (l && !l.contains(el) && txt(l) && txt(l).length < 70) return txt(l);
    }
    return '';
  };
  const out = [];
  const sel = 'input, select, textarea, mat-select, mat-checkbox, mat-slide-toggle, mat-radio-group';
  root.querySelectorAll(sel).forEach((el, i) => {
    if (!vis(el)) return;
    const tag = el.tagName.toLowerCase();
    if (tag === 'input' && el.closest('mat-checkbox, mat-slide-toggle, mat-radio-group, mat-select')) return;
    // the device list on the left and its search row are not the device's settings
    if (/^(name|ip|tags|select_box_)\d+|^select_all|^Setup-input$/.test(el.id || '') || el.closest('#table-panel, thead')) return;
    if (el.getAttribute('type') === 'search') return;
    if (['select_channel','select_flow','radio_primary-input','radio_secondary-input'].includes(el.id) || el.querySelector && el.querySelector('#radio_primary-input')) return;
    const r = {idx: i, tag, id: el.id || '', name: el.getAttribute('name') || el.getAttribute('formcontrolname') || '',
      label: labelOf(el), type: el.getAttribute('type') || '', min: el.getAttribute('min') || '', max: el.getAttribute('max') || '',
      maxlength: el.getAttribute('maxlength') || '', pattern: el.getAttribute('pattern') || '',
      disabled: !!(el.disabled || el.getAttribute('aria-disabled') === 'true' || el.classList.contains('mat-select-disabled') ||
                   el.classList.contains('mat-checkbox-disabled') || el.readOnly),
      value: '', options: []};
    if (tag === 'input' || tag === 'textarea') {
      r.value = el.type === 'checkbox' ? String(el.checked) : (el.value || el.getAttribute('placeholder') || '');
    } else if (tag === 'select') {
      r.options = [...el.options].map(o => [o.value, txt(o)]);
      r.value = el.selectedIndex >= 0 ? txt(el.options[el.selectedIndex]) : '';
    } else if (tag === 'mat-select') {
      r.value = txt(el.querySelector('.mat-select-value'));
    } else if (tag === 'mat-checkbox' || tag === 'mat-slide-toggle') {
      const inp = el.querySelector('input'); r.value = inp ? String(inp.checked) : '';
      r.label = r.label || txt(el);
    } else if (tag === 'mat-radio-group') {
      r.options = [...el.querySelectorAll('mat-radio-button')].map(b => [b.getAttribute('value') || b.getAttribute('ng-reflect-value') || '', txt(b)]);
      const c = el.querySelector('mat-radio-button.mat-radio-checked'); r.value = c ? txt(c) : '';
    }
    out.push(r);
  });
  return out;
}"""

rows = []


def scrape(pg, view, container_sel):
    root = pg.query_selector(container_sel)
    if not root:
        print("  (no container)", view)
        return
    items = root.evaluate(JS)
    # Material drop-downs: options exist only while open. Open, read, Escape.
    mats = root.query_selector_all("mat-select")
    mi = 0
    for it in items:
        if it["tag"] == "mat-select":
            el = mats[mi] if mi < len(mats) else None
            mi += 1
            if el and not it["disabled"]:
                try:
                    el.click(timeout=3000)
                    pg.wait_for_timeout(400)
                    it["options"] = [[o.get_attribute("ng-reflect-value") or "", (o.inner_text() or "").strip()]
                                     for o in pg.query_selector_all("mat-option")]
                finally:
                    pg.keyboard.press("Escape")
                    pg.wait_for_timeout(200)
        rows.append({"view": view, **{k: it[k] for k in ("label", "tag", "id", "name", "type", "value", "min", "max",
                                                            "maxlength", "pattern", "disabled")},
                     "options": " | ".join(f"{v}={t}" if v and v != t else t for v, t in it["options"])})
    print(f"  {view}: {len(items)} controls")


with sync_playwright() as p:
    b = p.chromium.launch(channel="chrome", headless=True)
    pg = b.new_page(viewport={"width": 1900, "height": 1400})
    pg.goto(URL, wait_until="networkidle", timeout=60000)
    pg.fill("#username", sec["user"]); pg.fill("#password", sec["pass"]); pg.click("#sign_in"); pg.wait_for_timeout(4000)
    pg.goto(URL + "#/device", wait_until="networkidle", timeout=60000); pg.wait_for_timeout(5000)
    # select the device row by its name placeholder
    idx = pg.evaluate("(n) => { const i=[...document.querySelectorAll('input[id^=name]')].find(e => e.placeholder===n); return i ? i.id.replace('name','') : null }", DEV)
    pg.click(f"#status_{idx}"); pg.wait_for_timeout(8000)
    # the device panel's tab group: the one whose labels include "PTP"
    group = pg.evaluate("""() => { const l=[...document.querySelectorAll('.mat-tab-label')].find(e => e.innerText.trim()==='PTP');
                                   return l ? l.id.split('-')[3] : null }""")
    labels = pg.query_selector_all(f"[id^='mat-tab-label-{group}-']")
    names = [(l.get_attribute("id"), (l.inner_text() or "").strip()) for l in labels]
    for lid, name in names:
        pg.click("#" + lid); pg.wait_for_timeout(2500)
        scrape(pg, "Device > " + name, "body")
    # Signals: channel x leg x flow. The three selectors only choose what is shown; nothing is applied.
    sig = pg.evaluate("() => [...document.querySelectorAll('.mat-tab-label')].filter(e=>e.innerText.trim()==='Signals').map(e=>e.id)")
    pg.click("#" + sig[-1]); pg.wait_for_timeout(5000)
    chans = pg.evaluate("() => [...document.querySelectorAll('#select_channel option')].map(o=>o.label||o.text)")
    for ch in chans:
        pg.select_option("#select_channel", label=ch); pg.wait_for_timeout(2500)
        for leg, radio in (("RED primary", "#radio_primary-input"), ("BLUE secondary", "#radio_secondary-input")):
            pg.locator(radio).check(force=True); pg.wait_for_timeout(1500)
            flows = pg.evaluate("() => [...document.querySelectorAll('#select_flow option')].map(o=>o.label||o.text)")
            for fl in flows:
                pg.select_option("#select_flow", label=fl); pg.wait_for_timeout(1800)
                scrape(pg, f"Signals > CH{ch} > {leg} > {fl}", "body")
    pg.screenshot(path=SP + "/mnset-last.png", full_page=True)
    b.close()

cols = ["view", "label", "tag", "id", "name", "type", "value", "options", "min", "max", "maxlength", "pattern", "disabled"]
with open(OUT, "w", newline="", encoding="utf-8") as f:
    w = csv.DictWriter(f, cols); w.writeheader(); w.writerows(rows)
print(len(rows), "controls ->", OUT)
