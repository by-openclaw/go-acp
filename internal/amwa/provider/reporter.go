package provider

import (
	"log/slog"

	"dhs/internal/amwa/codec/spec"
)

// logReporter writes compliance events to the Node's logger.
//
// The repo-wide posture is "absorb the deviation, keep running, fire an
// event — never silently". Absorbing was the easy half; without a
// Reporter wired in production the event went nowhere, which is exactly
// the silence the rule forbids. This is the smallest honest sink: an
// operator reading the Node's log sees every deviation its peers commit.
type logReporter struct{ logger *slog.Logger }

// newLogReporter returns a Reporter backed by logger, or nil when there
// is no logger to write to — a nil spec.Reporter is a valid no-op
// everywhere it is accepted.
func newLogReporter(logger *slog.Logger) spec.Reporter {
	if logger == nil {
		return nil
	}
	return logReporter{logger: logger}
}

// Report logs one event. Severity maps onto slog levels so a plant's
// log pipeline can alert on Error without parsing the message.
func (r logReporter) Report(e spec.ComplianceEvent) {
	args := []any{
		"plugin", "amwa",
		"spec", e.SpecID,
		"api_ver", e.APIVer,
		"code", e.Code,
		"resource", e.Resource,
	}
	if e.PeerHost != "" {
		args = append(args, "peer", e.PeerHost)
	}
	switch e.Severity {
	case spec.SeverityError:
		r.logger.Error(e.Detail, args...)
	case spec.SeverityWarn:
		r.logger.Warn(e.Detail, args...)
	default:
		r.logger.Info(e.Detail, args...)
	}
}
