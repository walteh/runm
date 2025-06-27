package slogdevterm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/tree"
	"github.com/goforj/godump"
)

const (
	valueIcon = ""
)

// StructToTree converts any Go struct (or value) to a stunning lipgloss tree.
func StructToTree(v interface{}, styles *Styles, render renderFunc) string {
	return StructToTreeWithTitle(v, "struct", styles, render)
}

// StructToTreeWithTitle creates a beautifully formatted struct display with tabwriter alignment
func StructToTreeWithTitle(v interface{}, title string, styles *Styles, render renderFunc) string {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 1, ' ', 0)

	buildTabbedStructDisplay(tw, reflect.ValueOf(v), styles.Tree, render, "")
	tw.Flush()

	content := strings.TrimRight(buf.String(), "\n")
	if content == "" {
		return ""
	}

	// Create a beautiful box like error logs but with intelligent width
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Tree.Branch.GetForeground()).
		Padding(0, 1).
		MaxWidth(150) // Only set max width, let it size naturally

	// Title header
	titleStyle := lipgloss.NewStyle().
		Foreground(styles.Tree.Root.GetForeground()).
		Bold(true)

	header := titleStyle.Render(title)
	fullContent := header + "\n" + content

	return boxStyle.Render(fullContent)
}

type renderFunc func(s lipgloss.Style, str string) string

// buildTabbedStructDisplay creates perfectly aligned output using tabwriter
func buildTabbedStructDisplay(tw *tabwriter.Writer, v reflect.Value, styles TreeStyles, render renderFunc, indent string) {
	if !v.IsValid() {
		return
	}

	// Handle pointers
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			fmt.Fprintf(tw, "%s%s\n", indent, render(styles.Null, "nil"))
			return
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		numFields := v.NumField()

		for i := 0; i < numFields; i++ {
			field := t.Field(i)
			fieldValue := v.Field(i)

			// Skip unexported fields
			if !field.IsExported() {
				continue
			}

			// Skip empty/nil values to reduce noise
			if isEmptyValue(fieldValue) {
				continue
			}

			if isSimpleType(fieldValue) {
				// Use tabwriter for perfect alignment: key \t = \t value
				key := render(styles.Key, field.Name)
				value := renderSimpleValue(fieldValue.Interface(), styles, render)
				fmt.Fprintf(tw, "%s%s\t=\t%s\n", indent, key, value)
			} else {
				// Complex value
				fmt.Fprintf(tw, "%s%s:\n", indent, render(styles.Key, field.Name))
				buildTabbedStructDisplay(tw, fieldValue, styles, render, indent+"  ")
			}
		}

	case reflect.Slice, reflect.Array:
		length := v.Len()
		for i := 0; i < length; i++ {
			item := v.Index(i)

			// Skip empty items
			if isEmptyValue(item) {
				continue
			}

			if isSimpleType(item) {
				indexStr := render(styles.Index, fmt.Sprintf("[%d]", i))
				value := renderSimpleValue(item.Interface(), styles, render)
				fmt.Fprintf(tw, "%s%s\t=\t%s\n", indent, indexStr, value)
			} else {
				fmt.Fprintf(tw, "%s%s:\n", indent, render(styles.Index, fmt.Sprintf("[%d]", i)))
				buildTabbedStructDisplay(tw, item, styles, render, indent+"  ")
			}
		}

	case reflect.Map:
		keys := v.MapKeys()
		for _, key := range keys {
			mapValue := v.MapIndex(key)

			// Skip empty values
			if isEmptyValue(mapValue) {
				continue
			}

			keyStr := fmt.Sprint(key.Interface())
			if isSimpleType(mapValue) {
				keyFormatted := render(styles.Key, keyStr)
				value := renderSimpleValue(mapValue.Interface(), styles, render)
				fmt.Fprintf(tw, "%s%s\t=\t%s\n", indent, keyFormatted, value)
			} else {
				fmt.Fprintf(tw, "%s%s:\n", indent, render(styles.Key, keyStr))
				buildTabbedStructDisplay(tw, mapValue, styles, render, indent+"  ")
			}
		}
	}
}

// renderSimpleValue renders values with beautiful colors and styling
func renderSimpleValue(v interface{}, styles TreeStyles, render renderFunc) string {
	if v == nil {
		return render(styles.Null, "nil")
	}

	switch vv := v.(type) {
	case string:
		// Use a more subtle string color
		return render(styles.String, fmt.Sprintf(`"%s"`, vv))
	case bool:
		if vv {
			// Green for true
			return render(styles.Bool, "true")
		} else {
			// Red for false (but still show it as it's important)
			return render(styles.Bool, "false")
		}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		// Nice number styling
		return render(styles.Number, fmt.Sprint(vv))
	case float32, float64:
		// Nice float styling
		return render(styles.Number, fmt.Sprint(vv))
	default:
		// Show type info for complex types
		return render(styles.Struct, fmt.Sprintf("(%T)", vv))
	}
}

// JSONToTreeWithTitle parses a JSON byte slice and returns a stunning lipgloss tree.
func JSONToTreeWithTitle(data []byte, keyName string, styles *Styles, render renderFunc) string {
	// Unmarshal into an empty interface
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return render(styles.Tree.Container, fmt.Sprintf("(unable to render JSON: %s) %s", err, string(data)))
	}

	t := tree.Root("󰘦 " + keyName).
		RootStyle(styles.Tree.Root).
		EnumeratorStyle(styles.Tree.Branch).
		Enumerator(tree.RoundedEnumerator).
		ItemStyle(styles.Tree.Key)

	buildJSONTreeNode(t, raw, styles.Tree, render)

	return render(styles.Tree.Container, t.String())
}

// buildStructTreeNode recursively builds a lipgloss tree from a struct using reflection
func buildStructTreeNode(node *tree.Tree, v reflect.Value, styles TreeStyles, render renderFunc) {
	if !v.IsValid() {
		return
	}

	// Handle pointers
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			node.Child(render(styles.Null, "nil"))
			return
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		numFields := v.NumField()

		for i := 0; i < numFields; i++ {
			field := t.Field(i)
			fieldValue := v.Field(i)

			// Skip unexported fields
			if !field.IsExported() {
				continue
			}

			// Skip empty/nil values to reduce noise
			if isEmptyValue(fieldValue) {
				continue
			}

			if isSimpleType(fieldValue) {
				// Simple value - show as key: value
				keyStr := render(styles.Key, valueIcon+" "+field.Name)
				valueStr := renderValue(fieldValue.Interface(), styles, render)
				node.Child(keyStr + ": " + valueStr)
			} else {
				// Complex value - create subtree
				keyStr := render(styles.Key, valueIcon+" "+field.Name)
				subTree := tree.New().Root(keyStr).
					RootStyle(styles.Key).
					EnumeratorStyle(styles.Branch).
					Enumerator(tree.RoundedEnumerator).
					ItemStyle(styles.Key)
				buildStructTreeNode(subTree, fieldValue, styles, render)
				node.Child(subTree)
			}
		}

	case reflect.Slice, reflect.Array:
		length := v.Len()
		for i := 0; i < length; i++ {
			item := v.Index(i)

			// Skip empty items
			if isEmptyValue(item) {
				continue
			}

			indexStr := render(styles.Index, fmt.Sprintf("󰅪 [%d]", i))

			if isSimpleType(item) {
				valueStr := renderValue(item.Interface(), styles, render)
				node.Child(indexStr + ": " + valueStr)
			} else {
				subTree := tree.New().Root(indexStr).
					RootStyle(styles.Index).
					EnumeratorStyle(styles.Branch).
					Enumerator(tree.RoundedEnumerator).
					ItemStyle(styles.Key)
				buildStructTreeNode(subTree, item, styles, render)
				node.Child(subTree)
			}
		}

	case reflect.Map:
		keys := v.MapKeys()
		for _, key := range keys {
			mapValue := v.MapIndex(key)

			// Skip empty values
			if isEmptyValue(mapValue) {
				continue
			}

			keyStr := render(styles.Key, valueIcon+" "+fmt.Sprint(key.Interface()))

			if isSimpleType(mapValue) {
				valueStr := renderValue(mapValue.Interface(), styles, render)
				node.Child(keyStr + ": " + valueStr)
			} else {
				subTree := tree.New().Root(keyStr).
					RootStyle(styles.Key).
					EnumeratorStyle(styles.Branch).
					ItemStyle(styles.Key)
				buildStructTreeNode(subTree, mapValue, styles, render)
				node.Child(subTree)
			}
		}
	}
}

// buildJSONTreeNode recursively builds a lipgloss tree from JSON data
func buildJSONTreeNode(node *tree.Tree, v interface{}, styles TreeStyles, render renderFunc) {
	switch vv := v.(type) {
	case map[string]interface{}:
		for key, jsonVal := range vv {
			keyStr := render(styles.Key, valueIcon+" "+key)

			if isSimpleJSONType(jsonVal) {
				valueStr := renderJSONValue(jsonVal, styles, render)
				node.Child(keyStr + ": " + valueStr)
			} else {
				subTree := tree.New().Root(keyStr).
					RootStyle(styles.Key).
					EnumeratorStyle(styles.Branch).
					Enumerator(tree.RoundedEnumerator).
					ItemStyle(styles.Key)
				buildJSONTreeNode(subTree, jsonVal, styles, render)
				node.Child(subTree)
			}
		}

	case []interface{}:
		for i, item := range vv {
			indexStr := render(styles.Index, fmt.Sprintf("󰅪 [%d]", i))

			if isSimpleJSONType(item) {
				valueStr := renderJSONValue(item, styles, render)
				node.Child(indexStr + ": " + valueStr)
			} else {
				subTree := tree.New().Root(indexStr).
					RootStyle(styles.Index).
					EnumeratorStyle(styles.Branch).
					Enumerator(tree.RoundedEnumerator).
					ItemStyle(styles.Key)
				buildJSONTreeNode(subTree, item, styles, render)
				node.Child(subTree)
			}
		}
	}
}

// isEmptyValue checks if a value is empty/nil and should be skipped in tree rendering
func isEmptyValue(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}

	switch v.Kind() {
	case reflect.Bool:
		// Skip false booleans - they're usually just defaults
		return false
	case reflect.Ptr, reflect.Interface:
		return v.IsNil()
	case reflect.String:
		return v.String() == ""
	case reflect.Slice, reflect.Array, reflect.Map:
		return v.Len() == 0
	case reflect.Struct:
		// Check if struct has any non-zero fields
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanInterface() && !isEmptyValue(v.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		// Skip zero numbers
		return v.Int() == 0 || v.Uint() == 0
	default:
		return v.IsZero()
	}
}

// isSimpleType checks if a reflect.Value represents a simple/primitive type
func isSimpleType(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}

	switch v.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.String:
		return true
	case reflect.Ptr:
		if v.IsNil() {
			return true
		}
		return isSimpleType(v.Elem())
	default:
		return false
	}
}

// isSimpleJSONType checks if a JSON value is a simple/primitive type
func isSimpleJSONType(v interface{}) bool {
	switch v.(type) {
	case string, bool, float64, nil:
		return true
	default:
		return false
	}
}

// renderValue renders a Go value with appropriate styling
func renderValue(v interface{}, styles TreeStyles, render renderFunc) string {
	if v == nil {
		return render(styles.Null, " nil")
	}

	switch vv := v.(type) {
	case string:
		return render(styles.String, fmt.Sprintf(` "%s"`, vv))
	case bool:
		if vv {
			return render(styles.Bool, "󰱒 true")
		} else {
			return render(styles.Bool, "󰰋 false")
		}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return render(styles.Number, "󰎠 "+fmt.Sprint(vv))
	default:
		return render(styles.Struct, "󰙅 "+fmt.Sprintf("(%T)", vv))
	}
}

// renderJSONValue renders a JSON value with appropriate styling
func renderJSONValue(v interface{}, styles TreeStyles, render renderFunc) string {
	switch vv := v.(type) {
	case nil:
		return render(styles.Null, " null")
	case string:
		return render(styles.String, fmt.Sprintf(` "%s"`, vv))
	case bool:
		if vv {
			return render(styles.Bool, "󰱒 true")
		} else {
			return render(styles.Bool, "󰰋 false")
		}
	case float64:
		return render(styles.Number, "󰎠 "+strconv.FormatFloat(vv, 'g', -1, 64))
	default:
		return render(styles.Struct, "󰙅 "+fmt.Sprint(vv))
	}
}

// GoBoxDump creates a box with a title containing a godump formatted output of the value
func GoBoxDump(v interface{}, title string, styles *Styles, render renderFunc) string {
	// Use the godump package to format the value
	dumpOutput := godump.DumpStr(v)

	// Remove the header that godump adds since we'll use our own box title
	lines := strings.Split(dumpOutput, "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "<#dump //") {
		lines = lines[1:]
	}
	content := strings.Join(lines, "\n")

	if content == "" {
		return ""
	}

	// Create a beautiful box like error logs with intelligent width
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Tree.Branch.GetForeground()).
		Padding(0, 1).
		MaxWidth(150) // Only set max width, let it size naturally

	// Title header
	titleStyle := lipgloss.NewStyle().
		Foreground(styles.Tree.Root.GetForeground()).
		Bold(true)

	header := titleStyle.Render(title)
	fullContent := header + "\n" + content

	return boxStyle.Render(fullContent)
}
