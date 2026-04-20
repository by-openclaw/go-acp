package probel

// --- rx 007 : Maintenance Message ------------------------------------------

// MaintenanceFunction selects the controller-side action requested by
// rx 007. Spec: SW-P-88 §5.8 "Maintenance Message". Reference: TS
// rx/007/options.ts MaintenanceFunction enum.
type MaintenanceFunction uint8

const (
	// MaintHardReset forces the controller to completely reset, as if power
	// had just been applied (watchdog-style hardware reset).
	MaintHardReset MaintenanceFunction = 0x00
	// MaintSoftReset forces a software-only reset (re-init after database
	// download or main-loop restart). Hardware may not be re-initialised.
	MaintSoftReset MaintenanceFunction = 0x01
	// MaintClearProtects clears crosspoint protects on (matrix, level),
	// ignoring ownership. Acts as a "MASTER protect override".
	// MatrixID=0xFF → clear that level on ALL matrices; LevelID=0xFF → clear
	// ALL levels on that matrix; both 0xFF → clear everything.
	MaintClearProtects MaintenanceFunction = 0x02
	// MaintDatabaseTransfer (dual-processor controllers only) forces the
	// database to be transferred from ACTIVE to IDLE. No further bytes.
	MaintDatabaseTransfer MaintenanceFunction = 0x04
)

// MaintenanceParams carries the optional matrix/level fields that only
// appear when Function == MaintClearProtects.
//
// Reference: TS rx/007/params.ts MaintenanceMessageCommandParams.
type MaintenanceParams struct {
	Function MaintenanceFunction
	MatrixID uint8 // 0-19 or 0xFF = "all matrices" — only used for ClearProtects
	LevelID  uint8 // 0-15 or 0xFF = "all levels"   — only used for ClearProtects
}

// EncodeMaintenance builds the MAINTENANCE MESSAGE request. Payload size is
// variable and depends on the selected function:
//
// General form (2-byte DATA including ID) — any function except ClearProtects:
//
//	| Byte | Field                | Notes                                |
//	|------|----------------------|--------------------------------------|
//	|  1   | Maintenance function | 0x00/0x01/0x04 (ClearProtects uses ext form) |
//
// ClearProtects extended form (4-byte DATA including ID):
//
//	| Byte | Field                | Notes                                |
//	|------|----------------------|--------------------------------------|
//	|  1   | Function = 0x02      | MaintClearProtects                   |
//	|  2   | Matrix number        | 0-19 or 0xFF                         |
//	|  3   | Level number         | 0-15 or 0xFF                         |
//
// Spec: SW-P-88 §5.8. Reference: TS rx/007/command.ts.
func EncodeMaintenance(p MaintenanceParams) Frame {
	if p.Function == MaintClearProtects {
		return Frame{
			ID:      RxMaintenance,
			Payload: []byte{byte(p.Function), p.MatrixID, p.LevelID},
		}
	}
	return Frame{
		ID:      RxMaintenance,
		Payload: []byte{byte(p.Function)},
	}
}

// DecodeMaintenance parses a MAINTENANCE MESSAGE. Any function except
// ClearProtects leaves MatrixID and LevelID set to zero.
func DecodeMaintenance(f Frame) (MaintenanceParams, error) {
	if f.ID != RxMaintenance {
		return MaintenanceParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 1 {
		return MaintenanceParams{}, ErrShortPayload
	}
	p := MaintenanceParams{Function: MaintenanceFunction(f.Payload[0])}
	if p.Function == MaintClearProtects {
		if len(f.Payload) < 3 {
			return MaintenanceParams{}, ErrShortPayload
		}
		p.MatrixID = f.Payload[1]
		p.LevelID = f.Payload[2]
	}
	return p, nil
}
