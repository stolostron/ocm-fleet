package optionalserialization

import (
	"encoding/json"
	"optionalvalues"

	"github.com/stolostron/ocm-fleet/server/pkg/optional"
)

//domain:snapshot
type Snapshot struct { // want "snapshot.*contains.*optionalvalues.ID"
	Name optional.Optional[optionalvalues.ID]
}

func Serialization(value optional.Optional[optionalvalues.ID]) {
	_, _ = json.Marshal(value) // want "does not declare json serialization"
	_ = json.Unmarshal(nil, &value) // want "does not declare json serialization"
	_, _ = json.Marshal(struct{ Name optional.Optional[optionalvalues.ID] }{Name: value}) // want "does not declare json serialization"
	var boxed any = value
	_, _ = json.Marshal(boxed) // want "does not declare json serialization"
	_, _ = json.Marshal(optional.Some(1)) // want "does not declare json serialization"
}
