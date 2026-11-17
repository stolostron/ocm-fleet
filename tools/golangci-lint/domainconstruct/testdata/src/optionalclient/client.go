package optionalclient

import (
	"optionalflow"
	"optionalvalues"

	"github.com/stolostron/ocm-fleet/server/pkg/optional"
)

type Alias = optional.Optional[optionalvalues.ID]

func AcrossPackages(value Alias) Alias {
	optionalvalues.UseOptional(optionalflow.AbsentStorage())
	id, ok := optionalflow.Forward(value)
	if ok {
		optionalvalues.Use(id)
	}
	unchecked, _ := optionalflow.Forward(value)
	optionalvalues.Use(unchecked) // want "reason: unchecked-presence"
	return Alias{}
}
