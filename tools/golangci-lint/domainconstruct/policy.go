package domainconstruct

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
)

type policy struct {
	name      string
	typ       types.Type
	fact      *constructionFact
	functions map[*types.Func]bool
}

type policySet map[*types.TypeName]*policy

func (policies policySet) lookup(t types.Type) *policy {
	if p := policies.declaration(t); p != nil && len(p.fact.Constructors) != 0 {
		return p
	}
	return nil
}

func (policies policySet) declaration(t types.Type) *policy {
	if named := domainType(t); named != nil {
		return policies[named.Obj()]
	}
	return nil
}

// domainType gives aliases, pointers, and generic instances one policy identity.
func domainType(t types.Type) *types.Named {
	if t == nil {
		return nil
	}
	t = types.Unalias(t)
	for {
		pointer, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = types.Unalias(pointer.Elem())
	}
	if named, ok := t.(*types.Named); ok {
		return named.Origin()
	}
	return nil
}

// constructionFact carries the type's policy across package boundaries. Function
// permissions stay local to the package that owns the protected type.
type constructionFact struct {
	Constructors  []string
	ZeroValid     bool
	Serialization []string
	Snapshot      bool
}

func (*constructionFact) AFact() {}

func (*constructionFact) String() string { return "requires constructors" }

func (c *checker) collectPolicies() {
	for _, file := range c.pass.Files {
		for _, declaration := range file.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok || group.Tok != token.TYPE {
				continue
			}
			if len(group.Specs) > 1 && hasDomainDirective(group.Doc) {
				c.reportPolicy(group, "grouped-directive", "put domain directives on each type in a grouped declaration")
				continue
			}
			for _, spec := range group.Specs {
				spec := spec.(*ast.TypeSpec)
				doc := spec.Doc
				if doc == nil && len(group.Specs) == 1 {
					doc = group.Doc
				}
				c.collectPolicy(spec, doc)
			}
		}
	}
}

func (c *checker) collectPolicy(spec *ast.TypeSpec, doc *ast.CommentGroup) {
	fact, ok := c.parsePolicy(spec, doc)
	if !ok {
		return
	}
	object := c.pass.TypesInfo.Defs[spec.Name].(*types.TypeName)
	named, ok := object.Type().(*types.Named)
	if !ok || spec.Assign.IsValid() {
		c.reportPolicy(spec, "defined-type-required", "domain:constructors requires a defined type")
		return
	}
	if len(fact.Constructors) != 0 {
		switch named.Underlying().(type) {
		case *types.Basic, *types.Array, *types.Struct:
		default:
			c.reportPolicy(spec, "unsupported-representation", "domain:constructors requires a scalar, array, or struct type")
			return
		}
	}
	if len(fact.Constructors) != 0 && named.TypeParams().Len() != 0 {
		c.reportPolicy(spec, "generic-declaration", "domain:constructors does not support generic type declarations")
		return
	}
	p := &policy{
		name:      c.pass.Pkg.Name() + "." + object.Name(),
		typ:       object.Type(),
		fact:      fact,
		functions: make(map[*types.Func]bool),
	}
	for _, name := range fact.Constructors {
		object := c.constructor(name)
		if object == nil {
			c.reportPolicy(spec, "unknown-constructor", fmt.Sprintf("unknown constructor %s", name))
			return
		}
		function, ok := object.(*types.Func)
		if !ok {
			c.reportPolicy(spec, "constructor-not-function", fmt.Sprintf("%s is not a function", name))
			return
		}
		p.functions[function] = true
	}
	// A declared hydration hook owns the same representation writes as a
	// restoration constructor. Encoding hooks still consume valid values.
	for _, codec := range fact.Serialization {
		name := decoderMethod(codec)
		if method := codecMethod(named, name, true); method != nil && validDecoder(method.Type().(*types.Signature), codec) {
			p.functions[method.Origin()] = true
		}
	}
	c.policies[object] = p
	c.pass.ExportObjectFact(object, fact)
}

func (c *checker) parsePolicy(spec *ast.TypeSpec, doc *ast.CommentGroup) (*constructionFact, bool) {
	if doc == nil {
		return nil, false
	}
	fact := new(constructionFact)
	constructors, valid := false, true
	for _, comment := range doc.List {
		text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
		fields := strings.Fields(text)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "domain:") {
			continue
		}
		switch fields[0] {
		case "domain:constructors":
			if constructors || len(fields) == 1 {
				c.reportPolicy(spec, "invalid-constructors-directive", "list at least one constructor in a single domain:constructors directive")
				valid = false
			}
			constructors = true
			fact.Constructors = fields[1:]
		case "domain:zero-valid":
			if len(fields) != 1 || fact.ZeroValid {
				c.reportPolicy(spec, "invalid-zero-valid-directive", "use domain:zero-valid once, without arguments")
				valid = false
			}
			fact.ZeroValid = true
		case "domain:serialization":
			// Codec declarations are validated by domainserialization. Keeping
			// their facts here lets construction and zero flow share ownership.
			fact.Serialization = append(fact.Serialization, fields[1:]...)
			if len(fields) == 1 {
				fact.Serialization = append(fact.Serialization, "")
			}
		case "domain:snapshot":
			fact.Snapshot = true
			if len(fields) != 1 {
				c.reportPolicy(spec, "invalid-snapshot-directive", "use domain:snapshot without arguments")
				valid = false
			}
		default:
			c.reportPolicy(spec, "unknown-directive", fmt.Sprintf("unknown domain directive %s", fields[0]))
			valid = false
		}
	}
	if fact.ZeroValid && !constructors {
		c.reportPolicy(spec, "constructors-directive-required", "domain:zero-valid requires domain:constructors")
		valid = false
	}
	return fact, valid && (constructors || fact.Snapshot || len(fact.Serialization) != 0)
}

func (c *checker) constructor(name string) types.Object {
	receiver, method, isMethod := strings.Cut(name, ".")
	object := c.pass.Pkg.Scope().Lookup(receiver)
	if !isMethod || object == nil {
		return object
	}
	typeName, ok := object.(*types.TypeName)
	if !ok {
		return nil
	}
	object, _, _ = types.LookupFieldOrMethod(types.NewPointer(typeName.Type()), true, c.pass.Pkg, method)
	return object
}

func (c *checker) policy(t types.Type) *policy {
	named := domainType(t)
	if named == nil {
		return nil
	}
	object := named.Obj()
	if p, known := c.policies[object]; known {
		if p != nil && len(p.fact.Constructors) != 0 {
			return p
		}
		return nil
	}
	var fact constructionFact
	var p *policy
	if c.pass.ImportObjectFact(object, &fact) {
		p = &policy{name: object.Pkg().Name() + "." + object.Name(), typ: object.Type(), fact: &fact}
	}
	c.policies[object] = p
	return c.policies.lookup(t)
}

func (p *policy) supports(codec string) bool {
	if p == nil || len(p.fact.Constructors) == 0 || !slices.Contains(p.fact.Serialization, codec) {
		return false
	}
	decoder := codecMethod(p.typ, decoderMethod(codec), true)
	return decoder != nil && validDecoder(decoder.Type().(*types.Signature), codec)
}

func hasDomainDirective(doc *ast.CommentGroup) bool {
	if doc != nil {
		for _, comment := range doc.List {
			if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(comment.Text, "//")), "domain:") {
				return true
			}
		}
	}
	return false
}
