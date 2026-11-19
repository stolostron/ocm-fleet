package zeroflow

import "errors"

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

var sharedRaw string

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
