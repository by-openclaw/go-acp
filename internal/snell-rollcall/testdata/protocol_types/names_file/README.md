# Names files (Full Control — names in bulk)

Everything countable on the routing interface is named by a file rather than
by one command per name.

## Spec

The command answers with Data Transfer Params holding a filename and a
checksum computed from the names themselves. The client fetches the file over
the file service and verifies it.

## What these bytes show

    RETVALUE cmd=114 CMD_SALVO_NAMES_8_FILENAME  "RC_Files\SalvoNames_8.dat"  884728382
    RETVALUE cmd=115 CMD_SALVO_NAMES_32_FILENAME "RC_Files\SalvoNames_32.dat" 833267397

Two files for the same salvos: one of eight-character names, one of
thirty-two. A panel with a narrow button takes the short set.

## Why the file and not the commands

A plant with a thousand salvos would need a thousand commands to say what they
are called. The same pattern names sources, destinations, associations and
devices.

The checksum is not decoration. Where a controller names a file its own file
service will not serve — the Centra does this, because the path is in the
controller filesystem while each node file service is rooted at its own
directory — the connector falls back to one command per name and records
`rollcall_names_file_unreadable` to explain why that read was slow.
