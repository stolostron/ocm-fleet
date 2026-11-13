package domainconstruct

func (c *zeroChecker) payloadProblem(boxed payloadType, reason zeroReason) *zeroProblem {
	if boxed.Kind == payloadNamed {
		for object, p := range c.policies {
			if p != nil && !p.fact.ZeroValid && object.Pkg() != nil &&
				object.Pkg().Path() == boxed.Package && object.Name() == boxed.Name {
				return &zeroProblem{name: p.name, typ: p.typ, reason: reason, possible: true}
			}
		}
	}
	for _, field := range boxed.Fields {
		if problem := c.payloadProblem(field.Type, reason); problem != nil {
			problem.path = fieldPath(field.Name, problem.path)
			return problem
		}
	}
	return nil
}
