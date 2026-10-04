// The three mutating NcObject sequence methods — SetSequenceItem (1m5),
// AddSequenceItem (1m6), RemoveSequenceItem (1m7) — shared by the
// IS-12 and IS-14 servers the way setProperty is: one gate, so an item
// one API refuses the other refuses too. An incoming item goes through
// the checks a whole-sequence Set applies per item (the property's
// datatype — enum members, struct fields — and its effective
// constraint), and an applied change reports its MS-05-02 change type
// and item index to the IS-12 notification side.

package provider

import (
	"encoding/json"
	"fmt"

	"dhs/internal/amwa/codec/ms05"
)

// sequenceItem decodes and validates one incoming item for p.
func (s *IS14ConfigurationServer) sequenceItem(obj *configObject, p *configProperty, raw json.RawMessage) (any, ms05.NcMethodStatus, error) {
	if p.desc.IsReadOnly {
		return nil, ms05.NcMethodStatusReadonly,
			fmt.Errorf("property %s (%s) is readonly", propKey(p.desc.ID), p.desc.Name)
	}
	if len(raw) == 0 {
		return nil, ms05.NcMethodStatusParameterError, fmt.Errorf("value argument required")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, ms05.NcMethodStatusBadCommandFormat, err
	}
	if v == nil {
		return nil, ms05.NcMethodStatusParameterError,
			fmt.Errorf("property %s (%s): a sequence item cannot be null", propKey(p.desc.ID), p.desc.Name)
	}
	if p.desc.TypeName != nil {
		if msg := valueMismatch(*p.desc.TypeName, v); msg != "" {
			return nil, ms05.NcMethodStatusParameterError,
				fmt.Errorf("property %s (%s): %s", propKey(p.desc.ID), p.desc.Name, msg)
		}
	}
	if c := effectiveConstraint(obj, p); c != nil {
		if err := ms05.CheckConstraintValue(v, c); err != nil {
			return nil, ms05.NcMethodStatusParameterError,
				fmt.Errorf("property %s (%s): %v", propKey(p.desc.ID), p.desc.Name, err)
		}
	}
	return v, ms05.NcMethodStatusOk, nil
}

// sequenceSet replaces the item at index.
func (s *IS14ConfigurationServer) sequenceSet(obj *configObject, p *configProperty, index int, raw json.RawMessage) (ms05.NcMethodStatus, error) {
	v, st, err := s.sequenceItem(obj, p, raw)
	if err != nil {
		return st, err
	}
	s.mu.Lock()
	seq, _ := asSequence(p.value)
	if index < 0 || index >= len(seq) {
		s.mu.Unlock()
		return ms05.NcMethodStatusIndexOutOfBounds,
			fmt.Errorf("SetSequenceItem: index %d out of bounds (length %d)", index, len(seq))
	}
	next := make([]any, len(seq))
	copy(next, seq)
	next[index] = v
	p.value = next
	s.mu.Unlock()
	s.changed(obj, p, propertyChange{Type: ms05.NcPropertyChangeTypeSequenceItemChanged, Index: &index, Value: v})
	return ms05.NcMethodStatusOk, nil
}

// sequenceAdd appends an item and returns its index.
func (s *IS14ConfigurationServer) sequenceAdd(obj *configObject, p *configProperty, raw json.RawMessage) (int, ms05.NcMethodStatus, error) {
	v, st, err := s.sequenceItem(obj, p, raw)
	if err != nil {
		return 0, st, err
	}
	s.mu.Lock()
	seq, _ := asSequence(p.value)
	index := len(seq)
	next := make([]any, index, index+1)
	copy(next, seq)
	p.value = append(next, v)
	s.mu.Unlock()
	s.changed(obj, p, propertyChange{Type: ms05.NcPropertyChangeTypeSequenceItemAdded, Index: &index, Value: v})
	return index, ms05.NcMethodStatusOk, nil
}

// sequenceRemove drops the item at index.
func (s *IS14ConfigurationServer) sequenceRemove(obj *configObject, p *configProperty, index int) (ms05.NcMethodStatus, error) {
	if p.desc.IsReadOnly {
		return ms05.NcMethodStatusReadonly,
			fmt.Errorf("property %s (%s) is readonly", propKey(p.desc.ID), p.desc.Name)
	}
	s.mu.Lock()
	seq, _ := asSequence(p.value)
	if index < 0 || index >= len(seq) {
		s.mu.Unlock()
		return ms05.NcMethodStatusIndexOutOfBounds,
			fmt.Errorf("RemoveSequenceItem: index %d out of bounds (length %d)", index, len(seq))
	}
	next := make([]any, 0, len(seq)-1)
	next = append(next, seq[:index]...)
	next = append(next, seq[index+1:]...)
	p.value = next
	s.mu.Unlock()
	s.changed(obj, p, propertyChange{Type: ms05.NcPropertyChangeTypeSequenceItemRemoved, Index: &index})
	return ms05.NcMethodStatusOk, nil
}
