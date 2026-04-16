# Protocol Plugin Template

Copy this directory to `internal/protocol/{name}/` to add a new protocol.

## Checklist

```
[ ] 1. Copy this directory to internal/protocol/{name}/
[ ] 2. Rename types, structs, and comments — replace "Template" with your protocol name
[ ] 3. Implement Protocol interface in plugin.go
[ ] 4. Set ProtocolMeta.Name and ProtocolMeta.DefaultPort
[ ] 5. Add: func init() { protocol.Register(&TemplateFactory{}) }
[ ] 6. Add import _ "acp/internal/protocol/{name}" to cmd/acp/main.go
[ ] 7. Add import _ "acp/internal/protocol/{name}" to cmd/acp-srv/main.go
[ ] 8. Write unit tests in tests/unit/{name}/
[ ] 9. Add integration test env var to tests/integration/{name}/
[ ] 10. Add protocol reference doc to docs/protocols/ if available
[ ] 11. Update README.md protocols table
```

## Nothing Else Changes

CLI, API handlers, WebSocket hub, UI, storage, export/import, validator
all see only `IProtocol`. Your protocol is picked up automatically
once step 5 and steps 6-7 are done.
