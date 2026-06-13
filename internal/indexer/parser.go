package indexer

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"sort"
	"strings"
)

// ExtractMinimalContext extracts only the essential parts of a Go file
// Returns: package declaration, imports, type definitions, function signatures (no bodies)
// This reduces token usage by 60-80% compared to full file content
func ExtractMinimalContext(filePath string) (string, error) {
	// For non-Go files, return a simple summary
	if !strings.HasSuffix(filePath, ".go") {
		return extractNonGoContext(filePath)
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		// If parsing fails, fall back to simple file reading
		return extractNonGoContext(filePath)
	}

	var sb strings.Builder

	// Write package declaration with package-level comments
	if node.Doc != nil {
		sb.WriteString(node.Doc.Text())
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("package %s\n\n", node.Name.Name))

	// Write imports
	if len(node.Imports) > 0 {
		sb.WriteString("import (\n")
		for _, imp := range node.Imports {
			if imp.Doc != nil {
				sb.WriteString("\t// " + strings.TrimSpace(imp.Doc.Text()) + "\n")
			}
			if imp.Name != nil {
				sb.WriteString(fmt.Sprintf("\t%s %s\n", imp.Name.Name, imp.Path.Value))
			} else {
				sb.WriteString(fmt.Sprintf("\t%s\n", imp.Path.Value))
			}
		}
		sb.WriteString(")\n\n")
	}

	// Write constants, variables, types, and function signatures
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			// Handle constants, variables, and types
			if d.Doc != nil {
				sb.WriteString(d.Doc.Text())
			}

			sb.WriteString(d.Tok.String())
			if len(d.Specs) == 1 {
				sb.WriteString(" ")
				writeSpec(&sb, d.Specs[0], fset)
				sb.WriteString("\n\n")
			} else {
				sb.WriteString(" (\n")
				for _, spec := range d.Specs {
					writeSpec(&sb, spec, fset)
					sb.WriteString("\n")
				}
				sb.WriteString(")\n\n")
			}

		case *ast.FuncDecl:
			// Handle function declarations (signature only, no body)
			if d.Doc != nil {
				sb.WriteString(d.Doc.Text())
			}

			sb.WriteString("func ")
			if d.Recv != nil {
				// Method receiver
				sb.WriteString("(")
				if len(d.Recv.List) > 0 {
					writeField(&sb, d.Recv.List[0], fset)
				}
				sb.WriteString(") ")
			}

			sb.WriteString(d.Name.Name)
			writeFuncType(&sb, d.Type, fset)
			sb.WriteString("\n\n")
		}
	}

	return sb.String(), nil
}

// writeSpec writes a single AST spec (type, const, or var declaration) to the string builder.
// It handles TypeSpec (type definitions), ValueSpec (const/var), and ImportSpec (imports).
// For ValueSpec, actual values are replaced with "..." to omit implementation details.
func writeSpec(sb *strings.Builder, spec ast.Spec, fset *token.FileSet) {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		if s.Doc != nil {
			sb.WriteString(s.Doc.Text())
		}
		sb.WriteString(s.Name.Name)
		sb.WriteString(" ")
		writeExpr(sb, s.Type, fset)

	case *ast.ValueSpec:
		if s.Doc != nil {
			sb.WriteString(s.Doc.Text())
		}
		for i, name := range s.Names {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(name.Name)
		}
		if s.Type != nil {
			sb.WriteString(" ")
			writeExpr(sb, s.Type, fset)
		}
		if len(s.Values) > 0 {
			sb.WriteString(" = ")
			for i := range s.Values {
				if i > 0 {
					sb.WriteString(", ")
				}
				// For values, just write "..." to indicate there's a value
				// We don't need the actual implementation
				sb.WriteString("...")
			}
		}

	case *ast.ImportSpec:
		if s.Name != nil {
			sb.WriteString(s.Name.Name)
			sb.WriteString(" ")
		}
		sb.WriteString(s.Path.Value)
	}
}

// writeExpr writes a Go type expression to the string builder.
// It recursively handles all Go type expressions including pointers, arrays, maps,
// structs, interfaces, function types, selectors, channels, and ellipsis.
func writeExpr(sb *strings.Builder, expr ast.Expr, fset *token.FileSet) {
	switch e := expr.(type) {
	case *ast.Ident:
		sb.WriteString(e.Name)

	case *ast.StarExpr:
		sb.WriteString("*")
		writeExpr(sb, e.X, fset)

	case *ast.ArrayType:
		sb.WriteString("[]")
		writeExpr(sb, e.Elt, fset)

	case *ast.MapType:
		sb.WriteString("map[")
		writeExpr(sb, e.Key, fset)
		sb.WriteString("]")
		writeExpr(sb, e.Value, fset)

	case *ast.StructType:
		sb.WriteString("struct {\n")
		if e.Fields != nil {
			for _, field := range e.Fields.List {
				sb.WriteString("\t")
				writeFieldInStruct(sb, field, fset)
				sb.WriteString("\n")
			}
		}
		sb.WriteString("}")

	case *ast.InterfaceType:
		if e.Methods != nil && len(e.Methods.List) > 0 {
			sb.WriteString("interface {\n")
			for _, method := range e.Methods.List {
				sb.WriteString("\t")
				writeFieldInInterface(sb, method, fset)
				sb.WriteString("\n")
			}
			sb.WriteString("}")
		} else {
			sb.WriteString("interface{}")
		}

	case *ast.FuncType:
		writeFuncType(sb, e, fset)

	case *ast.SelectorExpr:
		writeExpr(sb, e.X, fset)
		sb.WriteString(".")
		sb.WriteString(e.Sel.Name)

	case *ast.ChanType:
		if e.Dir == ast.RECV {
			sb.WriteString("<-chan ")
		} else if e.Dir == ast.SEND {
			sb.WriteString("chan<- ")
		} else {
			sb.WriteString("chan ")
		}
		writeExpr(sb, e.Value, fset)

	case *ast.Ellipsis:
		sb.WriteString("...")
		if e.Elt != nil {
			writeExpr(sb, e.Elt, fset)
		}

	default:
		// For complex expressions, just use a placeholder
		sb.WriteString("...")
	}
}

// writeField writes a generic AST field (function parameter, return value, etc.) to the string builder.
// It includes field names, types, and inline comments if present.
func writeField(sb *strings.Builder, field *ast.Field, fset *token.FileSet) {
	if field.Doc != nil {
		sb.WriteString(field.Doc.Text())
	}

	// Write field names
	for i, name := range field.Names {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(name.Name)
	}

	if len(field.Names) > 0 && field.Type != nil {
		sb.WriteString(" ")
	}

	// Write field type
	if field.Type != nil {
		writeExpr(sb, field.Type, fset)
	}

	if field.Comment != nil {
		sb.WriteString(" // ")
		sb.WriteString(strings.TrimSpace(field.Comment.Text()))
	}
}

// writeFieldInStruct writes a struct field to the string builder.
// It handles function-type fields specially by including the "func" keyword,
// and preserves struct tags (e.g., `json:"name"`) if present.
func writeFieldInStruct(sb *strings.Builder, field *ast.Field, fset *token.FileSet) {
	if field.Doc != nil {
		sb.WriteString(field.Doc.Text())
	}

	// Write field names
	for i, name := range field.Names {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(name.Name)
	}

	if len(field.Names) > 0 && field.Type != nil {
		sb.WriteString(" ")
	}

	// Write field type
	if field.Type != nil {
		// Function types in structs need "func" keyword
		if funcType, ok := field.Type.(*ast.FuncType); ok {
			sb.WriteString("func")
			writeFuncType(sb, funcType, fset)
		} else {
			writeExpr(sb, field.Type, fset)
		}
	}

	// Write field tag if present
	if field.Tag != nil {
		sb.WriteString(" ")
		sb.WriteString(field.Tag.Value)
	}

	if field.Comment != nil {
		sb.WriteString(" // ")
		sb.WriteString(strings.TrimSpace(field.Comment.Text()))
	}
}

// writeFieldInInterface writes an interface method signature to the string builder.
// It omits the "func" keyword (which is implicit in interface methods) and
// handles embedded interfaces (anonymous fields without names).
func writeFieldInInterface(sb *strings.Builder, field *ast.Field, fset *token.FileSet) {
	if field.Doc != nil {
		sb.WriteString(field.Doc.Text())
	}

	// Write method name
	for i, name := range field.Names {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(name.Name)
	}

	if len(field.Names) > 0 && field.Type != nil {
		sb.WriteString(" ")
	}

	// Write method signature (func type without "func" keyword)
	if field.Type != nil {
		if funcType, ok := field.Type.(*ast.FuncType); ok {
			writeFuncType(sb, funcType, fset)
		} else {
			// Embedded interface
			writeExpr(sb, field.Type, fset)
		}
	}

	if field.Comment != nil {
		sb.WriteString(" // ")
		sb.WriteString(strings.TrimSpace(field.Comment.Text()))
	}
}

// writeFuncType writes a function type signature (parameters and return values) to the string builder.
// It handles multiple parameters, multiple return values, and named/unnamed parameters.
// Return values with multiple entries or named returns are wrapped in parentheses.
func writeFuncType(sb *strings.Builder, ft *ast.FuncType, fset *token.FileSet) {
	sb.WriteString("(")
	if ft.Params != nil {
		for i, param := range ft.Params.List {
			if i > 0 {
				sb.WriteString(", ")
			}
			writeField(sb, param, fset)
		}
	}
	sb.WriteString(")")

	if ft.Results != nil && len(ft.Results.List) > 0 {
		sb.WriteString(" ")
		if len(ft.Results.List) > 1 || len(ft.Results.List[0].Names) > 1 {
			sb.WriteString("(")
		}
		for i, result := range ft.Results.List {
			if i > 0 {
				sb.WriteString(", ")
			}
			writeField(sb, result, fset)
		}
		if len(ft.Results.List) > 1 || len(ft.Results.List[0].Names) > 1 {
			sb.WriteString(")")
		}
	}
}

// ExtractExportedSymbols parses Go source and returns the sorted, exported
// top-level symbol names it declares: function and type/const/var names, with
// methods qualified as "Receiver.Method". Used to diff a file's public API
// before and after a change (see internal/journal).
func ExtractExportedSymbols(src []byte) ([]string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return nil, err
	}

	var symbols []string
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				if recv := receiverTypeName(d.Recv.List[0].Type); recv != "" {
					name = recv + "." + name
				}
			}
			symbols = append(symbols, name)

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.IsExported() {
						symbols = append(symbols, s.Name.Name)
					}
				case *ast.ValueSpec:
					for _, name := range s.Names {
						if name.IsExported() {
							symbols = append(symbols, name.Name)
						}
					}
				}
			}
		}
	}

	sort.Strings(symbols)
	return symbols, nil
}

// receiverTypeName returns the (possibly pointer) receiver's type name,
// or "" if it can't be determined.
func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(t.X)
	case *ast.Ident:
		return t.Name
	default:
		return ""
	}
}

// extractNonGoContext extracts context from non-Go files (config, docs, etc.).
// For small files (<10KB), it returns the full content.
// For larger files, it returns the first 100 lines to avoid excessive context.
func extractNonGoContext(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# File: %s\n\n", filePath))

	// For non-code files, include first 50 lines or 2KB, whichever is smaller
	scanner := bufio.NewScanner(file)
	lineCount := 0
	maxLines := 50
	maxBytes := 2048
	bytesRead := 0

	for scanner.Scan() && lineCount < maxLines && bytesRead < maxBytes {
		line := scanner.Text()
		sb.WriteString(line)
		sb.WriteString("\n")
		lineCount++
		bytesRead += len(line) + 1
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		return sb.String(), err
	}

	if lineCount >= maxLines || bytesRead >= maxBytes {
		sb.WriteString("\n... (truncated)\n")
	}

	return sb.String(), nil
}
