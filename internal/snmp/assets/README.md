# internal/snmp/assets

Drop-zone for everything the SNMP connector will be built from. Nothing here
is code, and nothing here is read at runtime — the MIB compiler runs OFFLINE
(see `../CLAUDE.md`), so what lands in this folder shapes generated tables,
not the shipped binary.

| Folder | What goes in it |
| --- | --- |
| `mibs/` | vendor MIB source (`.mib` / `.txt` / `.my`), **plus every MIB they IMPORT** |
| `vendor/` | datasheets, manuals, protocol + integration guides |

## Two things worth knowing before pushing

**Bring the imports.** A MIB is not self-contained. `FOO-MIB` will start with
something like

```
IMPORTS
    enterprises, OBJECT-TYPE, Integer32  FROM SNMPv2-SMI
    DisplayString                        FROM SNMPv2-TC;
```

and none of it compiles without those files. Vendors usually ship a folder
with the whole set — push the folder, not the one file that looked relevant.
The standard IETF MIBs (SNMPv2-SMI, SNMPv2-TC, SNMPv2-CONF, IF-MIB, ...) are
worth including too, so a compile does not depend on what happens to be
installed on the machine doing it.

**Keep the vendor's filenames.** MIB imports resolve by MODULE name, and
tooling conventionally maps module to filename. Renaming files is the usual
reason a compile cannot find something that is sitting right there.

## Large files

`.pdf`, `.doc`, `.docx` and archives are LFS-tracked by `.gitattributes`, so
they commit as pointers automatically. MIBs are plain text and small — they
commit as ordinary blobs, which is what we want: they need to be diffable
when a firmware revision changes them.
