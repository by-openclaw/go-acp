// Package template is a stub for a new protocol plugin.
// Copy this directory to internal/protocol/{name}/ and implement.
package template

import (
	"context"
	"log/slog"

	"acp/internal/protocol"
)

// Register this plugin on import.
func init() {
	protocol.Register(&TemplateFactory{})
}

// TemplateFactory creates TemplateProtocol instances.
type TemplateFactory struct{}

func (f *TemplateFactory) Meta() protocol.ProtocolMeta {
	return protocol.ProtocolMeta{
		Name:        "TEMPLATE",   // change to your protocol name e.g. "ACMP"
		DefaultPort: 0,            // set default port
		Description: "Template protocol — replace this description",
	}
}

func (f *TemplateFactory) New(logger *slog.Logger) protocol.Protocol {
	return &TemplateProtocol{logger: logger}
}

// TemplateProtocol implements protocol.Protocol.
type TemplateProtocol struct {
	logger *slog.Logger
}

func (p *TemplateProtocol) Connect(ctx context.Context, ip string, port int) error {
	return protocol.ErrNotImplemented
}

func (p *TemplateProtocol) Disconnect() error {
	return nil
}

func (p *TemplateProtocol) GetDeviceInfo(ctx context.Context) (protocol.DeviceInfo, error) {
	return protocol.DeviceInfo{}, protocol.ErrNotImplemented
}

func (p *TemplateProtocol) GetSlotInfo(ctx context.Context, slot int) (protocol.SlotInfo, error) {
	return protocol.SlotInfo{}, protocol.ErrNotImplemented
}

func (p *TemplateProtocol) Walk(ctx context.Context, slot int) ([]protocol.Object, error) {
	return nil, protocol.ErrNotImplemented
}

func (p *TemplateProtocol) GetValue(ctx context.Context, req protocol.ValueRequest) (protocol.Value, error) {
	return protocol.Value{}, protocol.ErrNotImplemented
}

func (p *TemplateProtocol) SetValue(ctx context.Context, req protocol.ValueRequest, val protocol.Value) (protocol.Value, error) {
	return protocol.Value{}, protocol.ErrNotImplemented
}

func (p *TemplateProtocol) Subscribe(req protocol.ValueRequest, fn protocol.EventFunc) error {
	return protocol.ErrNotImplemented
}

func (p *TemplateProtocol) Unsubscribe(req protocol.ValueRequest) error {
	return nil
}
