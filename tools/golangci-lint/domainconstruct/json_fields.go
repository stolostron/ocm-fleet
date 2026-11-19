package domainconstruct

import (
	"go/types"
	"iter"
	"reflect"
	"slices"
	"strings"
	"unicode"
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

type jsonFieldCache map[*types.Struct]jsonFieldSelection

func (cache *jsonFieldCache) fields(shape *types.Struct, json bool) iter.Seq[structField] {
	return func(yield func(structField) bool) {
		if !json {
			for index := range shape.NumFields() {
				field := shape.Field(index)
				if field.Name() != "_" && !yield(structField{field: field, indices: []int{index}, path: field.Name()}) {
					return
				}
			}
			return
		}
		selection, known := (*cache)[shape]
		if !known {
			selection = findJSONFields(shape)
			if *cache == nil {
				*cache = make(jsonFieldCache)
			}
			(*cache)[shape] = selection
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

// JSON promotes fields from anonymous structs and chooses the shallowest
// field for each wire name. At equal depth a tagged field takes precedence.
// Ties at the winning depth and tag precedence omit the name entirely.
func findJSONFields(root *types.Struct) jsonFieldSelection {
	candidates := make(map[string]jsonFieldCandidate)
	active := make(map[*types.Struct]bool)
	work := workBudget(maxInferenceWork)
	complete := true
	var collect func(*types.Struct, []int, string, bool)
	collect = func(shape *types.Struct, indices []int, path string, indirect bool) {
		if active[shape] {
			return
		}
		active[shape] = true
		defer delete(active, shape)
		for index := range shape.NumFields() {
			if !work.take() {
				complete = false
				return
			}
			field := shape.Field(index)
			t := field.Type()
			pointer, embedsPointer := t.Underlying().(*types.Pointer)
			if embedsPointer {
				t = pointer.Elem()
			}
			embedded, structure := t.Underlying().(*types.Struct)
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
				indices:  append(slices.Clone(indices), index),
				path:     fieldPath(path, field.Name()),
				indirect: indirect,
			}
			if name == "" && promoted {
				collect(embedded, child.indices, child.path, indirect || embedsPointer)
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
				candidates[name] = jsonFieldCandidate{structField: child, tagged: tagged}
			case len(child.indices) == len(previous.indices) && tagged == previous.tagged:
				previous.conflict = true
				candidates[name] = previous
			}
		}
	}
	collect(root, nil, "", false)
	if !complete {
		return jsonFieldSelection{}
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

func validJSONName(name string) bool {
	for _, character := range name {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) &&
			!strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", character) {
			return false
		}
	}
	return name != ""
}
