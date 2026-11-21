package optionalconstruction

import (
	"optionalhelpers"
	"optionalvalues"

	"github.com/stolostron/ocm-fleet/server/pkg/optional"
)

func Construction(raw string, valid optionalvalues.ID) {
	_ = optional.Some(valid)
	_ = optional.Some(optionalvalues.NewID(raw))
	_ = optional.Some(optionalvalues.ID(raw)) // want "construct optionalvalues.ID through NewID"
	_ = optional.Some[optionalvalues.ID]("unchecked") // want "construct optionalvalues.ID through NewID"
	_ = optionalhelpers.Construct[optionalvalues.ID](raw) // want "construct optionalvalues.ID through NewID"
	_ = optional.Optional[optionalvalues.ID]{}
}
