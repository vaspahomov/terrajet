/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package types

import (
	"fmt"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	twtypes "github.com/muvaf/typewriter/pkg/types"
	"github.com/pkg/errors"

	"github.com/crossplane/terrajet/pkg/config"
	"github.com/crossplane/terrajet/pkg/types/comments"
	"github.com/crossplane/terrajet/pkg/types/name"
)

const (
	wildcard = "*"
)

// NewBuilder returns a new Builder.
func NewBuilder(pkg *types.Package) *Builder {
	return &Builder{
		Package:  pkg,
		comments: twtypes.Comments{},
	}
}

// Generated is a struct that holds generated types
type Generated struct {
	Types    []*types.Named
	Comments twtypes.Comments

	ForProviderType *types.Named
	AtProviderType  *types.Named
}

// Builder is used to generate Go type equivalence of given Terraform schema.
type Builder struct {
	Package *types.Package

	genTypes []*types.Named
	comments twtypes.Comments
}

type typeWrapper struct {
	types.Type
	isObservation bool
}

// Build returns parameters and observation types built out of Terraform schema.
func (g *Builder) Build(cfg *config.Resource) (Generated, error) {
	fp, ap, err := g.buildResource(cfg.TerraformResource, cfg, nil, nil, cfg.Kind)
	return Generated{
		Types:           g.genTypes,
		Comments:        g.comments,
		ForProviderType: fp,
		AtProviderType:  ap,
	}, errors.Wrapf(err, "cannot build the Types")
}

func (g *Builder) buildResource(res *schema.Resource, cfg *config.Resource, tfPath []string, xpPath []string, names ...string) (*types.Named, *types.Named, error) { //nolint:gocyclo
	// NOTE(muvaf): There can be fields in the same CRD with same name but in
	// different types. Since we generate the type using the field name, there
	// can be collisions. In order to be able to generate unique names consistently,
	// we need to process all fields in the same order all the time.
	keys := sortedKeys(res.Schema)

	paramTypeName, err := g.generateTypeName("Parameters", names...)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "cannot generate parameters type name of %s", fieldPath(names))
	}
	paramName := types.NewTypeName(token.NoPos, g.Package, paramTypeName, nil)

	obsTypeName, err := g.generateTypeName("Observation", names...)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "cannot generate observation type name of %s", fieldPath(names))
	}
	obsName := types.NewTypeName(token.NoPos, g.Package, obsTypeName, nil)

	// We insert them to the package scope so that the type name calculations in
	// recursive calls are checked against their upper level type's name as well.
	g.Package.Scope().Insert(paramName)
	g.Package.Scope().Insert(obsName)

	// Note(turkenh): We don't know how many number of fields would be a
	// parameter or an observation in advance, hence opted for not to
	// preallocate (//nolint:prealloc). But we know a rough upper bound,
	// which is, len(keys), should we still do a preallocation here? Leaving
	// as it is given performance is not big concern during code generation.
	var paramFields []*types.Var //nolint:prealloc
	var paramTags []string       //nolint:prealloc
	var obsFields []*types.Var   //nolint:prealloc
	var obsTags []string         //nolint:prealloc
	for _, snakeFieldName := range keys {
		sch := res.Schema[snakeFieldName]
		fieldName := name.NewFromSnake(snakeFieldName)
		comment, err := comments.New(sch.Description)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "cannot build comment for description: %s", sch.Description)
		}
		tfTag := fmt.Sprintf("%s,omitempty", fieldName.Snake)
		jsonTag := fmt.Sprintf("%s,omitempty", fieldName.LowerCamelComputed)

		// Terraform paths, e.g. { "lifecycle_rule", "*", "transition", "*", "days" } for https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket#lifecycle_rule
		tfPaths := append(tfPath, fieldName.Snake)
		// Crossplane paths, e.g. {"lifecycleRule", "*", "transition", "*", "days"}
		xpPaths := append(xpPath, fieldName.LowerCamel)
		// Canonical paths, e.g. {"LifecycleRule", "Transition", "Days"}
		cnPaths := append(names[1:], fieldName.Camel)

		for _, f := range cfg.LateInitializer.IgnoredFields {
			// Convert configuration input from Terraform path to canonical path
			// Todo(turkenh/muvaf): Replace with a simple string conversion
			//  like GetIgnoredCanonicalFields where we just make each word
			//  between points camel case using names.go utilities. If the path
			//  doesn't match anything, it's no-op in late-init logic anyway.
			if f == fieldPath(tfPaths) {
				cfg.LateInitializer.AddIgnoredCanonicalFields(fieldPath(cnPaths))
			}
		}

		fieldTypes, err := g.buildSchema(sch, cfg, tfPaths, xpPaths, append(names, fieldName.Camel))
		if err != nil {
			return nil, nil, errors.Wrapf(err, "cannot infer type from schema of field %s", fieldName.Snake)
		}
		for _, fieldTypeWrapper := range fieldTypes {
			fieldType := fieldTypeWrapper.Type
			if ref, ok := cfg.References[fieldPath(tfPaths)]; ok {
				comment.Reference = ref
				sch.Optional = true
			}

			fieldNameCamel := fieldName.Camel
			if sch.Sensitive {
				if fieldTypeWrapper.isObservation {
					cfg.Sensitive.AddFieldPath(fieldPathWithWildcard(tfPaths), "status.atProvider."+fieldPathWithWildcard(xpPaths))
					// Drop an observation field from schema if it is sensitive.
					// Data will be stored in connection details secret
					continue
				}
				sfx := "SecretRef"
				cfg.Sensitive.AddFieldPath(fieldPathWithWildcard(tfPaths), "spec.forProvider."+fieldPathWithWildcard(xpPaths)+sfx)
				// todo(turkenh): do we need to support other field types as sensitive?
				if fieldType.String() != "string" && fieldType.String() != "*string" {
					return nil, nil, fmt.Errorf(`got type %q for field %q, only types "string" and "*string" supported as sensitive`, fieldType.String(), fieldNameCamel)
				}
				// Replace a parameter field with secretKeyRef if it is sensitive.
				// If it is an observation field, it will be dropped.
				// Data will be loaded from the referenced secret key.
				fieldNameCamel += sfx

				tfTag = "-"
				fieldType = typeSecretKeySelector
				jsonTag = name.NewFromCamel(fieldNameCamel).LowerCamelComputed
				if sch.Optional {
					fieldType = types.NewPointer(typeSecretKeySelector)
					jsonTag += ",omitempty"
				}
			}
			field := types.NewField(token.NoPos, g.Package, fieldNameCamel, fieldType, false)
			if comment.TerrajetOptions.FieldTFTag != nil {
				tfTag = *comment.TerrajetOptions.FieldTFTag
			}
			if comment.TerrajetOptions.FieldJSONTag != nil {
				jsonTag = *comment.TerrajetOptions.FieldJSONTag
			}

			// NOTE(muvaf): If a field is not optional but computed, then it's
			// definitely an observation field.
			// If it's optional but also computed, then it means the field has a server
			// side default but user can change it, so it needs to go to parameters.
			switch {
			case fieldTypeWrapper.isObservation:
				obsFields = append(obsFields, field)
				obsTags = append(obsTags, fmt.Sprintf(`json:"%s" tf:"%s"`, jsonTag, tfTag))
			default:
				if sch.Optional {
					paramTags = append(paramTags, fmt.Sprintf(`json:"%s" tf:"%s"`, jsonTag, tfTag))
				} else {
					// Required fields should not have omitempty tag in json tag.
					// TODO(muvaf): This overrides user intent if they provided custom
					// JSON tag.
					paramTags = append(paramTags, fmt.Sprintf(`json:"%s" tf:"%s"`, strings.TrimSuffix(jsonTag, ",omitempty"), tfTag))
				}
				req := !sch.Optional
				comment.Required = &req
				paramFields = append(paramFields, field)
			}
			if ref, ok := cfg.References[fieldPath(tfPaths)]; ok {
				refFields, refTags := g.generateReferenceFields(paramName, field, ref)
				paramTags = append(paramTags, refTags...)
				paramFields = append(paramFields, refFields...)
			}

			g.comments.AddFieldComment(paramName, fieldNameCamel, comment.Build())
		}
	}

	// NOTE(muvaf): Not every struct has both computed and configurable fields,
	// so some types we generate here are empty and unnecessary. However,
	// there are valid types with zero fields and we don't have the information
	// to differentiate between valid zero fields and unnecessary one. So we generate
	// two structs for every complex type.
	// See usage of wafv2EmptySchema() in aws_wafv2_web_acl here:
	// https://github.com/hashicorp/terraform-provider-aws/blob/main/aws/wafv2_helper.go#L13
	paramType := types.NewNamed(paramName, types.NewStruct(paramFields, paramTags), nil)
	g.genTypes = append(g.genTypes, paramType)

	obsType := types.NewNamed(obsName, types.NewStruct(obsFields, obsTags), nil)
	g.genTypes = append(g.genTypes, obsType)

	return paramType, obsType, nil
}

func (g *Builder) buildSchema(sch *schema.Schema, cfg *config.Resource, tfPath []string, xpPath []string, names []string) ([]typeWrapper, error) { // nolint:gocyclo
	switch sch.Type {
	case schema.TypeBool:
		return []typeWrapper{{types.NewPointer(types.Universe.Lookup("bool").Type()), isObservation(sch)}}, nil
	case schema.TypeFloat:
		return []typeWrapper{{types.NewPointer(types.Universe.Lookup("float64").Type()), isObservation(sch)}}, nil
	case schema.TypeInt:
		return []typeWrapper{{types.NewPointer(types.Universe.Lookup("int64").Type()), isObservation(sch)}}, nil
	case schema.TypeString:
		return []typeWrapper{{types.NewPointer(types.Universe.Lookup("string").Type()), isObservation(sch)}}, nil
	case schema.TypeMap, schema.TypeList, schema.TypeSet:
		tfPath = append(tfPath, wildcard)
		xpPath = append(xpPath, wildcard)
		ts := make([]typeWrapper, 0)
		var err error
		switch et := sch.Elem.(type) {
		case schema.ValueType:
			switch et {
			case schema.TypeBool:
				ts = []typeWrapper{{types.Universe.Lookup("bool").Type(), isObservation(sch)}}
			case schema.TypeFloat:
				ts = []typeWrapper{{types.Universe.Lookup("float64").Type(), isObservation(sch)}}
			case schema.TypeInt:
				ts = []typeWrapper{{types.Universe.Lookup("int64").Type(), isObservation(sch)}}
			case schema.TypeString:
				ts = []typeWrapper{{types.Universe.Lookup("string").Type(), isObservation(sch)}}
			case schema.TypeMap, schema.TypeList, schema.TypeSet, schema.TypeInvalid:
				return nil, errors.Errorf("element type of %s is basic but not one of known basic types", fieldPath(names))
			}
		case *schema.Schema:
			ts, err = g.buildSchema(et, cfg, tfPath, xpPath, names)
			if err != nil {
				return nil, errors.Wrapf(err, "cannot infer type from schema of element type of %s", fieldPath(names))
			}
		case *schema.Resource:
			paramType, obsType, err := g.buildResource(et, cfg, tfPath, xpPath, names...)
			if err != nil {
				return nil, errors.Wrapf(err, "cannot infer type from resource schema of element type of %s", fieldPath(names))
			}
			ts = make([]typeWrapper, 0)
			ts = append(ts, typeWrapper{obsType, true})
			if !isObservation(sch) {
				ts = append(ts, typeWrapper{paramType, false})
			}
			elemTypes := make([]typeWrapper, 0)
			for _, t := range ts {
				// NOTE(muvaf): Maps and slices are already pointers, so we don't need to
				// wrap them even if they are optional.
				if sch.Type == schema.TypeMap {
					elemTypes = append(elemTypes, typeWrapper{types.NewMap(types.Universe.Lookup("string").Type(), t), t.isObservation})
				} else {
					elemTypes = append(elemTypes, typeWrapper{types.NewSlice(t), t.isObservation})
				}
			}
			return elemTypes, nil
		// if unset
		// see: https://github.com/crossplane/terrajet/issues/177
		case nil:
			elemType := types.Universe.Lookup("string").Type()
			return []typeWrapper{{elemType, isObservation(sch)}}, nil
		default:
			return nil, errors.Errorf("element type of %s should be either schema.Resource or schema.Schema", fieldPath(names))
		}
		elemTypes := make([]typeWrapper, 0)
		for _, t := range ts {
			// NOTE(muvaf): Maps and slices are already pointers, so we don't need to
			// wrap them even if they are optional.
			if sch.Type == schema.TypeMap {
				elemTypes = append(elemTypes, typeWrapper{types.NewMap(types.Universe.Lookup("string").Type(), t), isObservation(sch)})
			} else {
				elemTypes = append(elemTypes, typeWrapper{types.NewSlice(t), isObservation(sch)})
			}
		}
		return elemTypes, nil
	case schema.TypeInvalid:
		return nil, errors.Errorf("invalid schema type %s", sch.Type.String())
	}
	return nil, errors.Errorf("unexpected schema type %s", sch.Type.String())
}

// generateTypeName generates a unique name for the type if its original name
// is used by another one. It adds the former field names recursively until it
// finds a unique name.
func (g *Builder) generateTypeName(suffix string, names ...string) (string, error) {
	n := names[len(names)-1] + suffix
	for i := len(names) - 2; i >= 0; i-- {
		if g.Package.Scope().Lookup(n) == nil {
			return n, nil
		}
		n = names[i] + n
	}
	if g.Package.Scope().Lookup(n) == nil {
		return n, nil
	}
	// start from 2 considering the 1st of this type is the one without an
	// index.
	for i := 2; i < 10; i++ {
		nn := fmt.Sprintf("%s_%d", n, i)
		if g.Package.Scope().Lookup(nn) == nil {
			return nn, nil
		}
	}
	return "", errors.Errorf("could not generate a unique name for %s", n)
}

func isObservation(s *schema.Schema) bool {
	return s.Computed && !s.Optional
}

func sortedKeys(m map[string]*schema.Schema) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	sort.Strings(keys)
	return keys
}

func fieldPath(parts []string) string {
	seg := make(fieldpath.Segments, len(parts))
	for i, p := range parts {
		if p == wildcard {
			continue
		}
		seg[i] = fieldpath.Field(p)
	}
	return seg.String()
}

func fieldPathWithWildcard(parts []string) string {
	seg := make(fieldpath.Segments, len(parts))
	for i, p := range parts {
		seg[i] = fieldpath.Field(p)
	}
	return seg.String()
}
