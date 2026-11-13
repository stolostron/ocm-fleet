package zeroflow

import "errors"

const RawEmpty = ""

func ZeroComparisons(id ID, optional *ID, total Total) {
	_ = id == ""       // want "zero comparison.*zeroflow.ID"
	_ = "" != id       // want "zero comparison.*zeroflow.ID"
	_ = id == RawEmpty // want "zero comparison.*zeroflow.ID"
	_ = string(id) == "" // want "zero comparison.*zeroflow.ID"
	_ = len(id) == 0 // want "zero comparison.*zeroflow.ID"
	_ = optional == nil
	_ = total == 0
	_ = id == "nonempty"
	switch id {
	case "": // want "zero comparison.*zeroflow.ID"
	case "nonempty":
	}
}

func RawZeroComparisons(raw string) bool { return raw == "" }

func ConsumingExplicitZeroStorage() {
	id := ID("")
	Accept(id) // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]ID\""
	ref := Ref{}
	AcceptRef(ref) // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]Ref\""
}

func MustID(raw string) ID {
	id, err := NewID(raw)
	if err != nil {
		panic(err)
	}
	return id
}

func MustPattern() { Accept(MustID("fixed")) }

// InspectionID allows its constructor to inspect the representation being built.
//domain:constructors NewInspectionID
type InspectionID string

func NewInspectionID(raw string) (InspectionID, error) {
	var zero InspectionID
	_ = string(zero)
	_ = len(zero)
	if raw == "" {
		return "", errors.New("empty ID")
	}
	return InspectionID(raw), nil
}

func InspectionOutsideConstructor() {
	var zero InspectionID
	_ = string(zero) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]InspectionID\""
	_ = len(zero) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]InspectionID\""
}

// NarrowID has an invalid zero, but truncating a valid value may also produce zero.
//domain:constructors NewNarrowID
type NarrowID int

func NewNarrowID(raw int) NarrowID {
	if raw == 0 { panic("empty ID") }
	return NarrowID(raw)
}

func NarrowingComparison(id NarrowID) bool { return uint8(id) == 0 }

func RepresentationComparison(id NarrowID) bool { return int(id) == 0 } // want "zero comparison.*zeroflow.NarrowID"
