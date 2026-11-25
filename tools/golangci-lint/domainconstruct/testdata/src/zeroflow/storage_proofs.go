package zeroflow

import "errors"

func clearNestedPointer(values [][]*string) {
	if pointer := values[0][0]; pointer != nil {
		*pointer = ""
	}
}

func MutateLaterNestedSlot(raw string, enabled bool) (ID, error) {
	inner := make([]*string, 1)
	outer := [][]*string{inner}
	if enabled {
		inner[0] = &raw
	}
	if raw == "" {
		return "", errors.New("empty")
	}
	clearNestedPointer(outer)
	return ID(raw), nil // want "return may produce an empty zeroflow.ID"
}

func MutateDisjointNestedSlot(raw string, enabled bool) (ID, error) {
	inner := make([]*string, 1)
	outer := [][]*string{inner}
	other := raw
	if enabled {
		inner[0] = &other
	}
	if raw == "" {
		return "", errors.New("empty")
	}
	clearNestedPointer(outer)
	return ID(raw), nil
}

type resettable string

func (value *resettable) Reset() { *value = "" }

//domain:constructors NewMarked
type Marked struct {
	resettable
	mark int
}

func NewMarked(value resettable) Marked { return Marked{resettable: value, mark: 1} }

func PromotedResetOnlyWritesReceiver(valid resettable) Marked {
	out := NewMarked(valid)
	(*Marked).Reset(&out)
	return out
}

func ExactResetWritesZero(valid resettable) ID {
	(*resettable).Reset(&valid)
	return ID(valid) // want "return produces an empty zeroflow.ID"
}

func OverwrittenField() Ref {
	var ref Ref
	ref.key = "initialized"
	ref.key = ""
	return ref // want "return .*zero zeroflow.Ref"
}

func OverwriteAfterWholeStore(valid Ref) Ref {
	var ref Ref
	ref = valid
	ref.key = ""
	return ref // want "return .*zero zeroflow.Ref"
}

func FieldReinitialized() Ref {
	var ref Ref
	ref.key = ""
	ref.key = "initialized"
	return ref
}

func SiblingFieldKeepsNonzero() Marked {
	var value Marked
	value.mark = 1
	value.resettable = "initialized"
	value.resettable = ""
	return value
}

func PreviousIterationError(read func() error, stop func() bool) (ID, error) {
	previous := errors.New("initial")
	for {
		next := read()
		if next != nil && stop() {
			return "", previous // want "return produces an empty zeroflow.ID"
		}
		previous = next
	}
}

func CurrentIterationError(read func() error, stop func() bool) (ID, error) {
	for {
		next := read()
		if next != nil && stop() {
			return "", next
		}
	}
}

func SharedFill(length int, valid ID) []ID {
	values := make([]ID, length)
	writes := make(chan int)
	ack := make(chan struct{})
	go func(target []ID) {
		for range len(target) {
			index := <-writes
			target[index] = "" // want "assignment stores an empty zeroflow.ID"
			ack <- struct{}{}
		}
	}(values) // want "argument contains an empty zeroflow.ID"
	for index := 0; index < len(values); index++ {
		values[index] = valid
		writes <- index
		<-ack
	}
	return values // want "return may contain an empty zeroflow.ID"
}

func PrivateFill(length int, valid ID, writes chan<- int, ack <-chan struct{}) []ID {
	values := make([]ID, length)
	for index := 0; index < len(values); index++ {
		values[index] = valid
		writes <- index
		<-ack
	}
	return values
}

var published []ID

func PublishedFill(length int, valid ID, writes chan<- int, ack <-chan struct{}) []ID {
	values := make([]ID, length)
	for index := 0; index < len(values); index++ {
		values[index] = valid
	}
	published = values
	for index := 0; index < len(values); index++ {
		values[index] = valid
		writes <- index
		<-ack
	}
	return values // want "return may contain an empty zeroflow.ID"
}

func PublishedWorker(writes <-chan int, ack chan<- struct{}) {
	for index := range writes {
		published[index] = ""
		ack <- struct{}{}
	}
}

func PublishedFillWithoutHandoff(length int, valid ID) []ID {
	values := make([]ID, length)
	for index := 0; index < len(values); index++ {
		values[index] = valid
	}
	published = values
	for index := 0; index < len(values); index++ {
		values[index] = valid
	}
	return values
}

func PublishedFillWithHandoffOnEarlyExit(length int, valid ID, stop bool, ack <-chan struct{}) []ID {
	values := make([]ID, length)
	for index := 0; index < len(values); index++ {
		values[index] = valid
	}
	published = values
	for index := 0; index < len(values); index++ {
		values[index] = valid
		if stop {
			<-ack
			return nil
		}
	}
	return values
}

func SharedJoinAfterHandoff() (ID, error) {
	statuses := make([]error, 1)
	start, done := make(chan struct{}), make(chan struct{})
	go func(target []error) {
		<-start
		clear(target)
		close(done)
	}(statuses)
	statuses[0] = errors.New("failure")
	close(start)
	<-done
	return "", errors.Join(statuses...) // want "return produces an empty zeroflow.ID"
}

func PrivateJoinAfterHandoff(start chan<- struct{}, done <-chan struct{}) (ID, error) {
	statuses := []error{errors.New("failure")}
	close(start)
	<-done
	return "", errors.Join(statuses...)
}

func SharedJoinAfterRepair() (ID, error) {
	statuses := make([]error, 1)
	done := make(chan struct{})
	go func(target []error) {
		clear(target)
		close(done)
	}(statuses)
	<-done
	statuses[0] = errors.New("failure")
	return "", errors.Join(statuses...)
}

var sharedRaw string

func clearGlobalOnly(_ *string) { sharedRaw = "" }

func BorrowedAfterGlobalWrite(target *string) (ID, error) {
	if *target == "" {
		return "", errors.New("empty")
	}
	clearGlobalOnly(target)
	return ID(*target), nil // want "return may produce an empty zeroflow.ID"
}

func PrivateAfterGlobalWrite(target string) (ID, error) {
	if target == "" {
		return "", errors.New("empty")
	}
	clearGlobalOnly(&target)
	return ID(target), nil
}

func initializeThenWriteGlobal(target *string) {
	*target = "valid"
	sharedRaw = ""
}

func BorrowedInitialization(target *string) ID {
	initializeThenWriteGlobal(target)
	return ID(*target) // want "return may produce an empty zeroflow.ID"
}

func PrivateInitialization() ID {
	var target string
	initializeThenWriteGlobal(&target)
	return ID(target)
}

func clearMapValue(values map[int]*string) { *values[0] = "" }

func MapMayReachGuardedValue(raw string, use func(ID)) {
	if raw != "" {
		clearMapValue(map[int]*string{0: &raw})
		use(ID(raw)) // want "argument may be an empty zeroflow.ID"
	}
}

func MapWithDisjointValue(raw, other string, use func(ID)) {
	if raw != "" {
		clearMapValue(map[int]*string{0: &other})
		use(ID(raw))
	}
}
