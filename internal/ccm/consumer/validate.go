package consumer

import (
	"context"
	"fmt"
	"net"
	"strings"

	dhsc "dhs/internal/consumer"
)

// ValidateValue checks one row of an import before anything is sent
// (consumer.ValueValidator): the checks a write makes — read-only, a word
// outside the enumeration, a number outside the bounds, a string past its
// length — so `import --check` reports them instead of a later write
// failing on them.
//
// One more check comes from the device rather than its document: the
// api.yml types an address as a plain string (no format, no pattern on
// SHUFFLE 6.0.0 or CONVERT 7.0.3), so a field that holds an IPv4 address
// now must be given one. That catches a shifted column or a name typed
// into a multicast cell; an empty field has nothing to compare and is
// left to the device.
func (p *Plugin) ValidateValue(ctx context.Context, req dhsc.ValueRequest, val dhsc.Value) error {
	_, spec, err := p.session()
	if err != nil {
		return err
	}
	resource, field, err := split(spec, req.Path)
	if err != nil {
		return fmt.Errorf("%w: %w", dhsc.ErrObjectNotFound, err)
	}
	if len(field) == 0 {
		return fmt.Errorf("%w: %s is a resource, not a value", dhsc.ErrValidationFailed, req.Path)
	}
	if tpl, ok := spec.TemplateFor(resource); !ok || !spec.Writable(tpl) {
		return fmt.Errorf("%w: %s: this device's api.yml declares no PUT or PATCH on %s", dhsc.ErrValidationFailed, req.Path, resource)
	}
	// A value the device already holds is left alone, whatever it is.
	live, lerr := p.GetValue(ctx, req)
	if lerr == nil && strings.TrimSpace(valueText(live)) == strings.TrimSpace(valueText(val)) {
		return nil
	}
	if err := checkWrite(spec, resource, field, val); err != nil {
		return err
	}
	if lerr == nil && isIPv4(live.Str) {
		if s := strings.TrimSpace(valueText(val)); s != "" && !isIPv4(s) {
			return fmt.Errorf("%w: %w", dhsc.ErrValidationFailed, &dhsc.ValidationError{
				Field:  strings.Join(append(segments(resource), field...), "."),
				Reason: fmt.Sprintf("%q is not an IPv4 address; the device holds %s here", s, live.Str),
			})
		}
	}
	return nil
}

// isIPv4 reports a dotted-quad IPv4 address, nothing looser.
func isIPv4(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil && !strings.Contains(s, ":")
}
