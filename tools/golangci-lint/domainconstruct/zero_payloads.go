package domainconstruct

func (c *zeroChecker) payloadProblem(boxed payloadType, reason zeroReason, use valueUse) *zeroProblem {
	if boxed.Kind == payloadNamed {
		for object, p := range c.policies {
			if p != nil && len(p.fact.Constructors) != 0 && !p.fact.ZeroValid && object.Pkg() != nil &&
				object.Pkg().Path() == boxed.Package && object.Name() == boxed.Name {
				return &zeroProblem{name: p.name, typ: p.typ, reason: reason, possible: true}
			}
		}
	}
	// An opaque boxed aggregate does not establish which fields a JSON hook or
	// the default encoder reads. Direct enrolled roots were checked above.
	if use.json() && (boxed.Kind == payloadStruct || boxed.Kind == payloadNamed) {
		return nil
	}
	for _, field := range boxed.Fields {
		if problem := c.payloadProblem(field.Type, reason, use); problem != nil {
			problem.path = fieldPath(field.Name, problem.path)
			return problem
		}
	}
	return nil
}
