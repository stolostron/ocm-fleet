package arrayconstruction

import "encodingvalues"

func Mutate(value *encodingvalues.ArrayID) {
	value[0] = 1 // want "modify encodingvalues.ArrayID through"
	value[1]++ // want "modify encodingvalues.ArrayID through"
	_ = &value[2] // want "do not expose mutable contents of encodingvalues.ArrayID"
	_ = value[:] // want "do not expose mutable contents of encodingvalues.ArrayID"
}
