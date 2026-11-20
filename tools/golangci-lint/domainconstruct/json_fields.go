package domainconstruct

import (
	"go/types"
	"iter"
	"reflect"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/tools/go/types/typeutil"
)

type structField struct {
	field    *types.Var
	indices  []int
	path     string
	indirect bool
}

type jsonFieldSelection struct {
	fields   []structField
	complete bool
}

type jsonFieldCache map[types.Type]jsonFieldSelection

func (cache *jsonFieldCache) fields(t types.Type, json bool) iter.Seq[structField] {
	return func(yield func(structField) bool) {
		if !json {
			shape := t.Underlying().(*types.Struct)
			for index := range shape.NumFields() {
				field := shape.Field(index)
				if field.Name() != "_" && !yield(structField{field: field, indices: []int{index}, path: field.Name()}) {
					return
				}
			}
			return
		}
		t := types.Unalias(t)
		selection, known := (*cache)[t]
		if !known {
			selection = findJSONFields(t)
			if *cache == nil {
				*cache = make(jsonFieldCache)
			}
			(*cache)[t] = selection
		}
		if !selection.complete {
			return // Unseen fields may shadow the candidates we found.
		}
		for _, field := range selection.fields {
			if !yield(field) {
				return
			}
		}
	}
}

type jsonFieldCandidate struct {
	structField
	tagged   bool
	conflict bool
}

type jsonEmbeddedType struct {
	typ      types.Type
	indices  []int
	path     string
	indirect bool
	repeated bool
}

// JSON promotes fields from anonymous structs and chooses the shallowest
// field for each wire name. At equal depth a tagged field takes precedence.
// Ties at the winning depth and tag precedence omit the name entirely.
func findJSONFields(root types.Type) jsonFieldSelection {
	candidates := make(map[string]jsonFieldCandidate)
	var visited typeutil.Map
	work, identityWork := workBudget(maxInferenceWork), workBudget(maxInferenceWork)
	current := []jsonEmbeddedType{{typ: root}}
	for len(current) != 0 {
		var next []jsonEmbeddedType
		var queued typeutil.Map
		for _, parent := range current {
			if len(parent.indices) != 0 && visited.Set(parent.typ, true) != nil {
				continue
			}
			shape := parent.typ.Underlying().(*types.Struct)
			for index := range shape.NumFields() {
				if !work.take() {
					return jsonFieldSelection{}
				}
				field := shape.Field(index)
				t := field.Type()
				pointer, embedsPointer := t.Underlying().(*types.Pointer)
				if embedsPointer {
					t = pointer.Elem()
				}
				_, structure := t.Underlying().(*types.Struct)
				promoted := field.Embedded() && structure
				tag := reflect.StructTag(shape.Tag(index)).Get("json")
				if tag == "-" || !field.Exported() && !promoted {
					continue
				}

				name, _, _ := strings.Cut(tag, ",")
				if !validJSONName(name) {
					name = ""
				}
				child := structField{
					field:    field,
					indices:  append(slices.Clone(parent.indices), index),
					path:     fieldPath(parent.path, field.Name()),
					indirect: parent.indirect,
				}
				if name == "" && promoted {
					if visited.Len() == 0 {
						if !jsonTypeIdentityFits(root, &identityWork) {
							return jsonFieldSelection{}
						}
						visited.Set(root, true)
					}
					if !jsonTypeIdentityFits(t, &identityWork) {
						return jsonFieldSelection{}
					}
					// Repetition ties direct fields at the next level. Only the
					// first path from that level queues the embedded descendants.
					if position := queued.At(t); position != nil {
						next[position.(int)].repeated = true
					} else {
						queued.Set(t, len(next))
						next = append(next, jsonEmbeddedType{
							typ: t, indices: child.indices, path: child.path, indirect: parent.indirect || embedsPointer,
						})
					}
					continue
				}
				tagged := name != ""
				if !tagged {
					name = field.Name()
				}
				previous, found := candidates[name]
				switch {
				case !found || len(child.indices) < len(previous.indices) ||
					len(child.indices) == len(previous.indices) && tagged && !previous.tagged:
					candidates[name] = jsonFieldCandidate{structField: child, tagged: tagged, conflict: parent.repeated}
				case len(child.indices) == len(previous.indices) && tagged == previous.tagged:
					previous.conflict = true
					candidates[name] = previous
				}
			}
		}
		current = next
	}
	fields := make([]structField, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.conflict {
			fields = append(fields, candidate.structField)
		}
	}
	slices.SortFunc(fields, func(first, second structField) int { return slices.Compare(first.indices, second.indices) })
	return jsonFieldSelection{fields, true}
}

// Identity maps can traverse unnamed types and type arguments while hashing
// and comparing keys. Bound that work independently of JSON field selection.
func jsonTypeIdentityFits(t types.Type, work *workBudget) bool {
	if !work.take() {
		return false
	}
	switch t := types.Unalias(t).(type) {
	case *types.Named:
		for argument := range t.TypeArgs().Types() {
			if !jsonTypeIdentityFits(argument, work) {
				return false
			}
		}
	case *types.Struct:
		for field := range t.Fields() {
			if !jsonTypeIdentityFits(field.Type(), work) {
				return false
			}
		}
	case *types.Interface:
		for method := range t.Methods() {
			if !jsonTypeIdentityFits(method.Type(), work) {
				return false
			}
		}
	case *types.Signature:
		return jsonTypeIdentityFits(t.Params(), work) && jsonTypeIdentityFits(t.Results(), work)
	case *types.Tuple:
		for variable := range t.Variables() {
			if !jsonTypeIdentityFits(variable.Type(), work) {
				return false
			}
		}
	case *types.Map:
		return jsonTypeIdentityFits(t.Key(), work) && jsonTypeIdentityFits(t.Elem(), work)
	case interface{ Elem() types.Type }:
		return jsonTypeIdentityFits(t.Elem(), work)
	}
	return true
}

func validJSONName(name string) bool {
	for _, character := range name {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) &&
			!strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", character) {
			return false
		}
	}
	return name != ""
}
