package deprecations

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// helpers for AST traversal
func isCRDRoot(st *ast.StructType) bool {
	for _, f := range st.Fields.List {
		if sel, ok := f.Type.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "metav1" && sel.Sel.Name == "TypeMeta" {
				return true
			}
		}
	}
	return false
}

func findFieldByGoName(st *ast.StructType, name string) *ast.Field {
	for _, f := range st.Fields.List {
		for _, n := range f.Names {
			if n.Name == name {
				return f
			}
		}
	}
	return nil
}

func typeNameOf(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeNameOf(t.X)
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name + "." + t.Sel.Name
		}
		return t.Sel.Name
	default:
		return ""
	}
}

func jsonNameForField(f *ast.Field) string {
	if f.Tag == nil {
		// use lowercased name if available
		if len(f.Names) > 0 {
			return strings.ToLower(f.Names[0].Name[:1]) + f.Names[0].Name[1:]
		}
		return ""
	}
	s, err := strconv.Unquote(f.Tag.Value)
	if err != nil {
		return ""
	}
	tag := reflectStructTag(s)
	if j, ok := tag["json"]; ok {
		parts := strings.Split(j, ",")
		if parts[0] != "" && parts[0] != "-" {
			return parts[0]
		}
	}
	if len(f.Names) > 0 {
		return strings.ToLower(f.Names[0].Name[:1]) + f.Names[0].Name[1:]
	}
	return ""
}

// reflectStructTag extracts tag key/value pairs from a raw struct tag string
func reflectStructTag(tag string) map[string]string {
	m := map[string]string{}
	for tag != "" {
		// skip leading space
		tag = strings.TrimLeft(tag, " ")
		if tag == "" {
			break
		}
		// key
		i := strings.Index(tag, ":")
		if i < 0 {
			break
		}
		key := tag[:i]
		tag = tag[i+1:]
		if tag == "" || tag[0] != '"' {
			break
		}
		// quoted value
		j := 1
		for j < len(tag) {
			if tag[j] == '\\' {
				j += 2
				continue
			}
			if tag[j] == '"' {
				break
			}
			j++
		}
		if j >= len(tag) {
			break
		}
		val := tag[1:j]
		m[key] = val
		tag = tag[j+1:]
	}
	return m
}

func findStructByFieldNames(structs map[string]*ast.StructType, names []string) string {
	need := map[string]struct{}{}
	for _, n := range names {
		need[n] = struct{}{}
	}
	for typeName, st := range structs {
		have := map[string]struct{}{}
		for _, f := range st.Fields.List {
			for _, id := range f.Names {
				have[id.Name] = struct{}{}
			}
		}
		ok := true
		for n := range need {
			if _, found := have[n]; !found {
				ok = false
				break
			}
		}
		if ok {
			return typeName
		}
	}
	return ""
}

func extractPoorlyNamedDeprecatedFields(files []*ast.File, structs map[string]*ast.StructType) map[string][]string {
	result := make(map[string][]string)

	// Map to track fields with Deprecated prefix (these we already handle)
	deprecatedPrefixed := make(map[string]map[string]bool)
	for typeName, st := range structs {
		deprecatedPrefixed[typeName] = make(map[string]bool)
		for _, f := range st.Fields.List {
			if len(f.Names) > 0 && strings.HasPrefix(f.Names[0].Name, DeprecationPrefix) {
				deprecatedPrefixed[typeName][f.Names[0].Name] = true
			}
		}
	}

	// Scan through all AST nodes looking for struct types with field comments containing "Deprecated"
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}

			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}

				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}

				typeName := ts.Name.Name
				var foundFields []string

				for _, field := range st.Fields.List {
					if len(field.Names) == 0 {
						continue
					}

					fieldName := field.Names[0].Name

					// Skip fields that already have Deprecated prefix
					if strings.HasPrefix(fieldName, DeprecationPrefix) {
						continue
					}

					// Check if the field has a comment mentioning "Deprecated" or "deprecated"
					if field.Comment != nil {
						for _, comment := range field.Comment.List {
							if strings.Contains(strings.ToLower(comment.Text), DeprecationPrefix) {
								foundFields = append(foundFields, fieldName)
								break
							}
						}
					}
				}

				// Also check doc comments attached to fields
				if len(foundFields) == 0 && st.Fields != nil {
					for _, field := range st.Fields.List {
						if len(field.Names) == 0 {
							continue
						}
						fieldName := field.Names[0].Name

						// Skip fields that already have Deprecated prefix
						if strings.HasPrefix(fieldName, DeprecationPrefix) {
							continue
						}

						// Check doc comments
						if field.Doc != nil {
							for _, comment := range field.Doc.List {
								if strings.Contains(strings.ToLower(comment.Text), strings.ToLower(DeprecationPrefix)) {
									foundFields = append(foundFields, fieldName)
									break
								}
							}
						}
					}
				}

				if len(foundFields) > 0 {
					result[typeName] = foundFields
				}
			}
		}
	}

	return result
}

func literalForFieldType(fieldTypeName string, isPtr bool) string {
	if fieldTypeName != "" {
		if isPtr {
			return fmt.Sprintf("&%s{}", fieldTypeName)
		}
		return fmt.Sprintf("%s{}", fieldTypeName)
	}
	return "struct{}{}"
}

func literalForTypeExpr(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		// pointer to some type
		if id, ok := t.X.(*ast.Ident); ok {
			name := id.Name
			// default: non-nil pointer to empty struct
			return fmt.Sprintf("&%s{}", name)
		}
		if sel, ok := t.X.(*ast.SelectorExpr); ok {
			return fmt.Sprintf("&%s.%s{}", sel.X.(*ast.Ident).Name, sel.Sel.Name)
		}
	case *ast.Ident:
		switch t.Name {
		case "string":
			return "\"deprecated\""
		case "bool":
			return "true"
		case "int", "int32", "int64":
			return "1"
		default:
			return fmt.Sprintf("%s{}", t.Name)
		}
	case *ast.SelectorExpr:
		// Qualified identifier like pkg.Type -> pkg.Type{}
		if x, ok := t.X.(*ast.Ident); ok {
			return fmt.Sprintf("%s.%s{}", x.Name, t.Sel.Name)
		}
		return fmt.Sprintf("%s{}", t.Sel.Name)
	}
	return "nil"
}

func buildNodeFromFieldRefs(refs []fieldRef) *node {
	tmp := &node{children: map[string]*node{}}
	for _, dr := range refs {
		cur := tmp
		for i, seg := range dr.GoPath {
			isLast := i == len(dr.GoPath)-1
			if cur.children[seg] == nil {
				cur.children[seg] = &node{children: map[string]*node{}}
			}
			cur = cur.children[seg]
			if isLast {
				cur.typeExpr = dr.TypeExpr
			}
		}
	}
	return tmp
}

func extractFieldType(st *ast.StructType, fname string) (typeName string, isPtr bool) {
	if st == nil {
		return "", false
	}
	f := findFieldByGoName(st, fname)
	if f == nil {
		return "", false
	}

	if star, ok := f.Type.(*ast.StarExpr); ok {
		isPtr = true
		if id, ok := star.X.(*ast.Ident); ok {
			typeName = id.Name
		} else if sel, ok := star.X.(*ast.SelectorExpr); ok {
			typeName = sel.Sel.Name
		}
	} else if id, ok := f.Type.(*ast.Ident); ok {
		typeName = id.Name
	} else if sel, ok := f.Type.(*ast.SelectorExpr); ok {
		typeName = sel.Sel.Name
	}
	return typeName, isPtr
}

func extractSpecType(specField *ast.Field) string {
	if specField == nil {
		return ""
	}
	specType := typeNameOf(specField.Type)
	if strings.Contains(specType, ".") {
		parts := strings.Split(specType, ".")
		specType = parts[len(parts)-1]
	}
	return specType
}
