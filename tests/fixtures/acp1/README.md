# ACP1 Test Fixtures

Byte-exact packet captures and expected-output files for ACP1 codec tests.

## Files

- `getobject_root.bin`     — raw getObject(root,0) reply from the emulator
- `getobject_float.bin`    — raw getObject(control,91) reply (GainA float)
- `announce_frame.bin`     — raw frame-status announcement

Add `.bin` files here by capturing with Wireshark on port 2071 and
saving the ACP payload (UDP data only, no IP/UDP headers).

Tests in `tests/unit/acp1/` load these fixtures via `os.ReadFile`.
