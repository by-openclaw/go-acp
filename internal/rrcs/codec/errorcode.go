package codec

import (
	"errors"
	"fmt"
)

// ErrorCode is the result code carried by almost every RRCS answer (§7).
type ErrorCode int

const (
	CodeSuccess                 ErrorCode = 0
	CodeTransKeyInvalid         ErrorCode = 1
	CodeNetInvalid              ErrorCode = 2
	CodeNodeInvalid             ErrorCode = 3
	CodePortInvalid             ErrorCode = 4
	CodeSlotInvalid             ErrorCode = 5
	CodeInputGainInvalid        ErrorCode = 6
	CodeIPAddressInvalid        ErrorCode = 7
	CodeTCPPortInvalid          ErrorCode = 8
	CodeLabelInvalid            ErrorCode = 9
	CodeConferencePosInvalid    ErrorCode = 10
	CodeArtistNotConnected      ErrorCode = 11
	CodeRouteDoesNotExist       ErrorCode = 12
	CodeGatewayStandby          ErrorCode = 13
	CodeParametersWrong         ErrorCode = 14
	CodeConferenceInvalid       ErrorCode = 15
	CodeConferenceMemberInvalid ErrorCode = 16
	CodePriorityInvalid         ErrorCode = 17
	CodeGPIONumberInvalid       ErrorCode = 18
	CodeGainInvalid             ErrorCode = 19
	CodeTimeout                 ErrorCode = 20
	CodeNoPermission            ErrorCode = 21
	CodeObjectDoesNotExist      ErrorCode = 22
	CodeNoDongle                ErrorCode = 23
	CodePortNotOnline           ErrorCode = 24
	CodePropertyNotSupported    ErrorCode = 25
	CodeLimitExceeded           ErrorCode = 26
	CodeGeneric                 ErrorCode = 99
)

// codeText is the §7 table, wording as printed.
var codeText = map[ErrorCode]string{
	CodeSuccess:                 "Success",
	CodeTransKeyInvalid:         "Transaction key invalid",
	CodeNetInvalid:              "Net address invalid",
	CodeNodeInvalid:             "Node address invalid",
	CodePortInvalid:             "Port address invalid",
	CodeSlotInvalid:             "Slot no. invalid",
	CodeInputGainInvalid:        "Input Gain invalid",
	CodeIPAddressInvalid:        "IP-address invalid",
	CodeTCPPortInvalid:          "TCP-Port invalid",
	CodeLabelInvalid:            "Label invalid",
	CodeConferencePosInvalid:    "Conference position invalid",
	CodeArtistNotConnected:      "Operation failed, because Artist-network not connected",
	CodeRouteDoesNotExist:       "Operation failed, because route does not exist",
	CodeGatewayStandby:          "Operation not possible, because gateway is standby",
	CodeParametersWrong:         "XML-RPC parameters wrong for this request",
	CodeConferenceInvalid:       "Invalid conference or conference not found",
	CodeConferenceMemberInvalid: "Invalid conference member or conference member not found",
	CodePriorityInvalid:         "Invalid priority",
	CodeGPIONumberInvalid:       "Invalid GPIO number",
	CodeGainInvalid:             "Invalid gain value",
	CodeTimeout:                 "Timeout",
	CodeNoPermission:            "No permission",
	CodeObjectDoesNotExist:      "Object does not exist",
	CodeNoDongle:                "No USB-dongle available",
	CodePortNotOnline:           "Port is not online",
	CodePropertyNotSupported:    "Object property not supported",
	CodeLimitExceeded:           "Limit exceeded",
	CodeGeneric:                 "Generic error",
}

// Known reports whether the code is in the §7 table. A newer RRCS may
// send codes this table does not have; they are carried, not refused.
func (c ErrorCode) Known() bool {
	_, ok := codeText[c]
	return ok
}

// String is the §7 wording, or "error code N" for a code not in the table.
func (c ErrorCode) String() string {
	if s, ok := codeText[c]; ok {
		return s
	}
	return fmt.Sprintf("error code %d", int(c))
}

// Err is nil for success and a *CodeError otherwise.
func (c ErrorCode) Err() error {
	if c == CodeSuccess {
		return nil
	}
	return &CodeError{Code: c}
}

// CodeError is a non-zero error code returned by RRCS. Callers match it
// with errors.Is against a CodeError of the same code, or read the code
// with errors.As.
type CodeError struct {
	Code ErrorCode
}

func (e *CodeError) Error() string {
	return fmt.Sprintf("rrcs: %s (code %d)", e.Code, int(e.Code))
}

// Is matches another CodeError carrying the same code.
func (e *CodeError) Is(target error) bool {
	var t *CodeError
	return errors.As(target, &t) && t.Code == e.Code
}
