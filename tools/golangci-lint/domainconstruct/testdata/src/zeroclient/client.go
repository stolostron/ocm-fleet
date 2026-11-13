package zeroclient

import renamed "zeroflow"

type LocalID = renamed.ID

func CrossPackage() {
	var id LocalID
	renamed.Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	var total renamed.Total
	renamed.AcceptTotal(total)
}

func Forward(raw string) (renamed.ID, error) { return renamed.NewID(raw) }

func MissingField() {
	renamed.AcceptRequest(renamed.Request{}) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\"; path: \"ID\""
}

func InvalidPointer() {
	renamed.AcceptPointer(new(renamed.ID)) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func SentinelFailure() (renamed.Ref, error) { return renamed.Ref{}, renamed.ErrMissing }

func WrappedFailure(err error) (renamed.Ref, error) {
	if err != nil {
		return renamed.Ref{}, renamed.ForwardTerminal(err)
	}
	return renamed.NewRef("valid")
}

func ImportedFactories() {
	renamed.Accept(renamed.Zero[renamed.ID]())                // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	renamed.AcceptPointer(renamed.FreshPointer[renamed.ID]()) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	renamed.AcceptIDs(renamed.FreshSlice[renamed.ID](2))      // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func ImportedBoxedFailure() {
	payload, _ := renamed.StoredBoxedFailure()
	renamed.AcceptAny(payload) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}
