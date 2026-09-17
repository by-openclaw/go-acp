package is09

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// Spec-strict ranges from
// https://specs.amwa.tv/is-09/releases/v1.0.0/APIs/schemas/with-refs/global.html.
const (
	HeartbeatIntervalMin      = 1
	HeartbeatIntervalMax      = 1000
	HeartbeatIntervalDefault  = 5
	AnnounceReceiptTimeoutMin = 2
	AnnounceReceiptTimeoutMax = 10
	PTPDomainMin              = 0
	PTPDomainMax              = 127
	SyslogV1DefaultPort       = 514
	SyslogV2DefaultPort       = 6514
	SyslogPortMin             = 1
	SyslogPortMax             = 65535
)

// uuidRE enforces RFC 4122 v1-v5 form per resource_core.json.
var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// versionRE enforces the IS-04 TAI-timestamp form `<sec>:<nsec>`.
var versionRE = regexp.MustCompile(`^[0-9]+:[0-9]+$`)

// Validate runs every spec rule on the resource and returns a chained
// error listing every violation found (concatenated with "; ").
// Encoder + decoder funnel through this.
func (g *Global) Validate() error {
	var errs []string

	// resource_core
	if g.ID == "" || !uuidRE.MatchString(g.ID) {
		errs = append(errs, fmt.Sprintf("id %q: must match RFC 4122 v1-v5 UUID pattern", g.ID))
	}
	if g.Version == "" || !versionRE.MatchString(g.Version) {
		errs = append(errs, fmt.Sprintf("version %q: must match `<sec>:<nsec>` TAI form", g.Version))
	}
	if g.Label == "" {
		errs = append(errs, "label: required (resource_core)")
	}
	if g.Description == "" {
		errs = append(errs, "description: required (resource_core)")
	}
	if g.Tags == nil {
		errs = append(errs, "tags: required (may be empty object, but key must be present)")
	}

	// IS-09 globals — required nesteds
	if g.IS04.HeartbeatInterval < HeartbeatIntervalMin || g.IS04.HeartbeatInterval > HeartbeatIntervalMax {
		errs = append(errs, fmt.Sprintf("is04.heartbeat_interval=%d: out of [%d..%d]",
			g.IS04.HeartbeatInterval, HeartbeatIntervalMin, HeartbeatIntervalMax))
	}
	if g.PTP.AnnounceReceiptTimeout < AnnounceReceiptTimeoutMin || g.PTP.AnnounceReceiptTimeout > AnnounceReceiptTimeoutMax {
		errs = append(errs, fmt.Sprintf("ptp.announce_receipt_timeout=%d: out of [%d..%d]",
			g.PTP.AnnounceReceiptTimeout, AnnounceReceiptTimeoutMin, AnnounceReceiptTimeoutMax))
	}
	if g.PTP.DomainNumber < PTPDomainMin || g.PTP.DomainNumber > PTPDomainMax {
		errs = append(errs, fmt.Sprintf("ptp.domain_number=%d: out of [%d..%d]",
			g.PTP.DomainNumber, PTPDomainMin, PTPDomainMax))
	}

	// Optional syslog blocks. Spec says hostname/port are individually
	// optional, but if either is set we range-check both per the
	// schema's port bounds.
	if g.Syslog != nil {
		if err := validateSyslogBlock("syslog", g.Syslog); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if g.SyslogV2 != nil {
		if err := validateSyslogBlock("syslogv2", g.SyslogV2); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("is09 /global validation failed: %s", strings.Join(errs, "; "))
}

func validateSyslogBlock(label string, s *SyslogConfig) error {
	if s == nil {
		return nil
	}
	if s.Hostname != "" {
		if !isValidSyslogHostname(s.Hostname) {
			return fmt.Errorf("%s.hostname %q: must be a hostname, IPv4, or IPv6", label, s.Hostname)
		}
	}
	if s.Port != 0 {
		if s.Port < SyslogPortMin || s.Port > SyslogPortMax {
			return fmt.Errorf("%s.port=%d: out of [%d..%d]", label, s.Port, SyslogPortMin, SyslogPortMax)
		}
	}
	return nil
}

// isValidSyslogHostname accepts any of the three formats listed in the
// global.json schema: hostname (RFC 1123), IPv4, IPv6.
func isValidSyslogHostname(s string) bool {
	if ip := net.ParseIP(s); ip != nil {
		return true
	}
	return looksLikeRFC1123Hostname(s)
}

// looksLikeRFC1123Hostname is a stdlib-only RFC 1123 hostname check —
// labels separated by dots, each 1-63 chars, alphanumeric or hyphen
// (not starting/ending with hyphen).
func looksLikeRFC1123Hostname(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	labels := strings.Split(s, ".")
	for _, lbl := range labels {
		if lbl == "" || len(lbl) > 63 {
			return false
		}
		if lbl[0] == '-' || lbl[len(lbl)-1] == '-' {
			return false
		}
		for i := 0; i < len(lbl); i++ {
			c := lbl[i]
			isAlpha := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
			isDigit := c >= '0' && c <= '9'
			if !isAlpha && !isDigit && c != '-' {
				return false
			}
		}
	}
	return true
}
