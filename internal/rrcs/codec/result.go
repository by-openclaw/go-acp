package codec

import "fmt"

// Result is the part common to RRCS answers: the transaction key of the
// request, echoed, and an error code.
//
// The specification prints two shapes. The short answers are an array
// whose first two elements are the key and the code (§11); the list
// answers are a struct with members named TransKey and ErrorCode (§8.1
// GetAllActiveXps, §8.7). Both are read here so a caller can check the
// outcome before looking at the payload.
type Result struct {
	TransKey string
	Code     ErrorCode

	// Rest holds what follows the key and the code in the array shape.
	Rest []Value
	// Fields holds the whole struct in the struct shape.
	Fields Value
}

// ParseResult reads the transaction key and the error code out of an
// answer value.
func ParseResult(v Value) (Result, error) {
	switch v.Kind {
	case KindArray:
		if len(v.Items) < 2 {
			return Result{}, fmt.Errorf("%w: answer array has %d elements, want a key and a code", ErrMalformed, len(v.Items))
		}
		key, err := v.Items[0].AsString()
		if err != nil {
			return Result{}, fmt.Errorf("transaction key: %w", err)
		}
		code, err := v.Items[1].AsInt()
		if err != nil {
			return Result{}, fmt.Errorf("error code: %w", err)
		}
		return Result{TransKey: key, Code: ErrorCode(code), Rest: v.Items[2:]}, nil
	case KindStruct:
		k, ok := v.Field("TransKey")
		if !ok {
			return Result{}, fmt.Errorf("%w: answer struct has no member %q", ErrMalformed, "TransKey")
		}
		key, err := k.AsString()
		if err != nil {
			return Result{}, fmt.Errorf("transaction key: %w", err)
		}
		code, err := fieldInt(v, "ErrorCode")
		if err != nil {
			return Result{}, err
		}
		return Result{TransKey: key, Code: ErrorCode(code), Fields: v}, nil
	}
	return Result{}, fmt.Errorf("%w: answer is a %s, want an array or a struct", ErrType, v.Kind)
}
