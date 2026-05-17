// Package template is a stub for a new protocol plugin.
// Copy this directory to internal/consumer/{name}/ and implement.
package template

import (
	"context"
	"log/slog"

	"dhs/internal/consumer"
)

// Register this plugin on import.
func init() {
	consumer.Register(&TemplateFactory{})
}

// TemplateFactory creates TemplateProtocol instances.
type TemplateFactory struct{}

func (f *TemplateFactory) Meta() consumer.ProtocolMeta {
	return consumer.ProtocolMeta{
		Name:        "TEMPLATE",   // change to your protocol name e.g. "ACMP"
		DefaultPort: 0,            // set default port
		Description: "Template protocol — replace this description",
	}
}

func (f *TemplateFactory) New(logger *slog.Logger) consumer.Protocol {
	return &TemplateProtocol{logger: logger}
}

// TemplateProtocol implements consumer.consumer.
type TemplateProtocol struct {
	logger *slog.Logger
}

func (p *TemplateProtocol) Connect(ctx context.Context, ip string, port int) error {
	return consumer.ErrNotImplemented
}

func (p *TemplateProtocol) Disconnect() error {
	return nil
}

func (p *TemplateProtocol) GetDeviceInfo(ctx context.Context) (consumer.DeviceInfo, error) {
	return consumer.DeviceInfo{}, consumer.ErrNotImplemented
}

func (p *TemplateProtocol) GetSlotInfo(ctx context.Context, slot int) (consumer.SlotInfo, error) {
	return consumer.SlotInfo{}, consumer.ErrNotImplemented
}

func (p *TemplateProtocol) Walk(ctx context.Context, slot int) ([]consumer.Object, error) {
	return nil, consumer.ErrNotImplemented
}

func (p *TemplateProtocol) GetValue(ctx context.Context, req consumer.ValueRequest) (consumer.Value, error) {
	return consumer.Value{}, consumer.ErrNotImplemented
}

func (p *TemplateProtocol) SetValue(ctx context.Context, req consumer.ValueRequest, val consumer.Value) (consumer.Value, error) {
	return consumer.Value{}, consumer.ErrNotImplemented
}

func (p *TemplateProtocol) Subscribe(req consumer.ValueRequest, fn consumer.EventFunc) error {
	return consumer.ErrNotImplemented
}

func (p *TemplateProtocol) Unsubscribe(req consumer.ValueRequest) error {
	return nil
}
