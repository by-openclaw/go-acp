package rollcall

import (
	"archive/zip"
	"bytes"
	"fmt"
	"time"

	"dhs/internal/snell-rollcall/codec"
)

// TemplateFileName is the archive the vendor's Control Panel reads before it
// draws a unit.
//
// The name is fixed and upper case; the entry inside is not upper case, and
// both matter. Taken from the vendor's own archives shipped with the Centra
// simulator, every one of which holds exactly one entry called Template.tpl.
const TemplateFileName = "TEMPLATE.ZIP"

// templateEntryName is the file inside the archive, spelled as the vendor
// spells it.
const templateEntryName = "Template.tpl"

// templateFormatVersion is the version line every vendor template carries.
const templateFormatVersion = 12

// templateAllLevels is the user-level mask in a section header.
//
// The vendor's own minimal templates use 15, which is every level. A panel
// connected at supervisor asks for level 4, and a template offering only some
// levels is answered with "No pages for the requested command set version
// and/or RollCall level".
const templateAllLevels = 15

// Control types, from TemplateDoc.html and confirmed against the vendor's own
// Template.tpl files. Negative codes are the PC-only widgets; the non-negative
// ones are the menu styles themselves.
const (
	ctlGroupBox  = -1  // CM_STATIC, a frame around a block
	ctlLabel     = -26 // CM_LEFTTITLE, static text
	ctlValueText = -18 // CM_LEFTTEXT, a value rendered as text
	ctlScrollbar = -9  // CM_HSCROLLBAR
	ctlEditBox   = -12 // CM_EDITBOX
	ctlPreset    = -14 // CM_PRESET, the little button that restores a default
	ctlCheckbox  = 64  // CM_CHECKBOX
)

// Layout, in the dialog units the vendor's own templates use.
const (
	tplPageWidth = 420
	tplRowHeight = 12
	tplTopMargin = 14
	tplLabelX    = 10
	tplLabelW    = 100
	tplPresetX   = 116
	tplPresetW   = 8
	tplControlX  = 128
	tplControlW  = 140
	tplValueX    = 276
	tplValueW    = 120
	tplRowH      = 8
)

// nl is the line ending every template line uses. Built from its code point
// so the source carries no escape of its own.
var nl = string(rune(10))

// templateEpoch is the timestamp every entry carries.
//
// A fixed one rather than the clock, because the archive is generated from the
// tree and two providers serving the same tree should serve the same bytes: a
// client caching by checksum would otherwise re-fetch it on every restart.
var templateEpoch = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

// buildTemplate renders one card as the archive a Control Panel reads.
//
// The format is the vendor's, not ours. It was previously a paragraph of prose
// in a file called template.txt, on the belief recorded here that "the vendor's
// binary layout is not documented anywhere we have". It is documented, in
// assets/Protocol/Docs/TemplateDoc.html, and the Centra simulator ships working
// examples: every one of their TEMPLATE.ZIP archives holds exactly one entry
// called Template.tpl.
//
// One archive per card, because that is how a real frame serves it — each
// node's file service is rooted at its own directory — and because two cards
// can carry different menus. Building one archive for the frame and keying its
// sections by card type gave the second card the first one's page, and a panel
// drew the first card's controls against the second card's commands.
func buildTemplate(prt *port) []byte {
	var body bytes.Buffer

	switch {
	case prt.level != nil:
		writeRouterLevelPages(&body, prt)
	case prt.matrix != nil:
		writeRouterMatrixPage(&body, prt)
	case prt.router != nil:
		writeXYPanelPage(&body, prt)
	default:
		fmt.Fprintf(&body, "[Version]%sversion=%d%s", nl, templateFormatVersion, nl)
		writeTemplatePage(&body, prt)
	}

	var out bytes.Buffer
	zw := zip.NewWriter(&out)

	// Nothing here can fail: the destination is a buffer in memory, and the
	// header is one we build ourselves.
	w, _ := zw.CreateHeader(&zip.FileHeader{
		Name:     templateEntryName,
		Method:   zip.Deflate,
		Modified: templateEpoch,
	})
	_, _ = w.Write(body.Bytes())
	_ = zw.Close()

	return out.Bytes()
}

// writeTemplatePage renders one card type as a single page of controls.
func writeTemplatePage(body *bytes.Buffer, prt *port) {
	lines := prt.menu(true)

	height := tplTopMargin + tplRowHeight*(len(lines)+1)
	fmt.Fprintf(body, "[%d:%d:%d:0]\n", prt.id.TypeID, prt.id.Version.CmdSet, templateAllLevels)
	fmt.Fprintf(body, "Size=0,0,%d,%d\n", tplPageWidth, height)

	n := 0
	ctl := func(caption string, command int64, flags, typ, x, y, w, h int) {
		fmt.Fprintf(body, "Ctl%d=%s,%d,%d,%d,%d,%d,%d,%d\n",
			n, caption, command, flags, typ, x, y, w, h)
		n++
	}

	ctl(prt.id.Name, -1, 0, ctlGroupBox, 2, 2, tplPageWidth-6, height-6)

	y := tplTopMargin
	for _, l := range lines {
		label := l.Text
		if label == "" {
			label = fmt.Sprintf("line %d", l.Index)
		}

		if l.Command == 0 {
			// A container names the block under it and carries no value.
			ctl(label, -1, 0, ctlLabel, tplLabelX, y, tplPageWidth-20, tplRowH)
			y += tplRowHeight
			continue
		}

		cmd := int64(l.Command)
		ctl(label, -1, 0, ctlLabel, tplLabelX, y, tplLabelW, tplRowH)

		switch l.Style.Kind() {
		case codec.StyleCheckbox:
			ctl(" ", cmd, 1, ctlCheckbox, tplControlX, y, 17, tplRowH)
		case codec.StyleEditString:
			ctl(label, cmd, 0, ctlEditBox, tplControlX, y, tplControlW, tplRowH)
		case codec.StyleNumber:
			if l.Style.Disabled() {
				ctl(label, cmd, 0, ctlValueText, tplControlX, y, tplControlW, tplRowH)
				break
			}
			// "P" restores the line's default. The vendor puts one beside
			// every scrollbar; a card without them can be driven but not put
			// back, which is half a control.
			ctl("P", cmd, 0, ctlPreset, tplPresetX, y, tplPresetW, tplRowH)
			ctl("New Scrollbar", cmd, 0, ctlScrollbar, tplControlX, y, tplControlW, tplRowH)
			ctl(label, cmd, 0, ctlValueText, tplValueX, y, tplValueW, tplRowH)
		default:
			ctl(label, cmd, 0, ctlValueText, tplControlX, y, tplControlW, tplRowH)
		}
		y += tplRowHeight
	}
	body.WriteString("\n")
}
