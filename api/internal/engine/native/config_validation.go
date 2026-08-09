package native

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ksuk/merlon/api/internal/engine"
	"gopkg.in/yaml.v3"
)

// configFieldError attaches the document path a validation failure belongs to.
//
// The validators used to return a bare message, so the API could say a
// configuration was invalid but not where. Wrapping rather than replacing the
// error keeps Error() byte-identical, so every existing caller, log line and
// test observes exactly what it did before.
type configFieldError struct {
	path string
	err  error
}

func (e *configFieldError) Error() string { return e.err.Error() }
func (e *configFieldError) Unwrap() error { return e.err }

// fieldErrorf builds a validation failure that knows its own location.
func fieldErrorf(path, format string, args ...any) error {
	return &configFieldError{path: path, err: fmt.Errorf(format, args...)}
}

// fieldPath returns the document path an error names, if it names one.
func fieldPath(err error) string {
	var fe *configFieldError
	if errors.As(err, &fe) {
		return fe.path
	}
	return ""
}

// yamlLinePattern matches the position yaml.v3 reports in its own messages
// ("yaml: line 4: did not find expected key"). The parser knows where it
// stopped; it just does not expose it structurally.
var yamlLinePattern = regexp.MustCompile(`line (\d+)`)

// syntaxPosition extracts the line a parse error refers to. Returning zero is
// correct when the parser did not say: a fabricated line is worse than none,
// because the operator will look at the wrong place and conclude the report is
// broken.
func syntaxPosition(err error) int {
	if err == nil {
		return 0
	}
	match := yamlLinePattern.FindStringSubmatch(err.Error())
	if match == nil {
		return 0
	}
	line, convErr := strconv.Atoi(match[1])
	if convErr != nil {
		return 0
	}
	return line
}

// configLocator resolves a dotted document path to the position of that key in
// the text the operator actually wrote.
type configLocator struct {
	root *yaml.Node
}

func newConfigLocator(content string) *configLocator {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		// A document that does not parse has no positions to resolve. Syntax
		// errors carry their own line, so there is nothing to recover here.
		return &configLocator{}
	}
	return &configLocator{root: &root}
}

var pathSegmentPattern = regexp.MustCompile(`^([^\[\]]*)((?:\[\d+\])*)$`)

// locate walks a path such as "entries[0].names" and returns the position of
// the final key. It resolves the path segment by segment rather than searching
// for a key name, so a nested "names" is never reported at the line of an
// unrelated "names" higher in the document.
func (l *configLocator) locate(path string) (line, column int, ok bool) {
	if l == nil || l.root == nil || path == "" {
		return 0, 0, false
	}

	node := l.root
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return 0, 0, false
		}
		node = node.Content[0]
	}

	// The position of the key is what an operator needs; the value may be on
	// another line, and for an empty value there is no value node at all.
	positioned := node

	for _, segment := range strings.Split(path, ".") {
		match := pathSegmentPattern.FindStringSubmatch(segment)
		if match == nil {
			return 0, 0, false
		}

		if key := match[1]; key != "" {
			keyNode, valueNode := mappingEntry(node, key)
			if keyNode == nil {
				return 0, 0, false
			}
			positioned = keyNode
			node = valueNode
		}

		for _, index := range parseIndices(match[2]) {
			if node == nil || node.Kind != yaml.SequenceNode || index >= len(node.Content) {
				return 0, 0, false
			}
			node = node.Content[index]
			positioned = node
		}
	}

	return positioned.Line, positioned.Column, true
}

func mappingEntry(node *yaml.Node, key string) (keyNode, valueNode *yaml.Node) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i], node.Content[i+1]
		}
	}
	return nil, nil
}

func parseIndices(suffix string) []int {
	if suffix == "" {
		return nil
	}
	var out []int
	for _, part := range strings.Split(strings.Trim(suffix, "[]"), "][") {
		if part == "" {
			continue
		}
		index, err := strconv.Atoi(part)
		if err != nil {
			continue
		}
		out = append(out, index)
	}
	return out
}

// configValidationCollector assembles a classified result. It exists so every
// call site records a class and a severity: an unclassified finding is exactly
// the bare message this work replaced.
type configValidationCollector struct {
	locator *configLocator
	result  *engine.ConfigValidationResult
}

func newConfigValidationCollector(content string) *configValidationCollector {
	return &configValidationCollector{
		locator: newConfigLocator(content),
		result:  &engine.ConfigValidationResult{Valid: true},
	}
}

// addSyntax records a document the parser rejected.
func (c *configValidationCollector) addSyntax(err error) {
	c.result.Valid = false
	c.result.Errors = append(c.result.Errors, engine.ConfigValidationError{
		Field:    "yaml",
		Message:  fmt.Sprintf("parse error: %v", err),
		Class:    engine.ConfigErrorSyntax,
		Severity: engine.ConfigSeverityError,
		Line:     syntaxPosition(err),
	})
}

// add records a classified failure, resolving its position from the path the
// error carries when it carries one.
func (c *configValidationCollector) add(field string, class engine.ConfigValidationErrorClass, err error) {
	entry := engine.ConfigValidationError{
		Field:    field,
		Message:  err.Error(),
		Class:    class,
		Severity: engine.ConfigSeverityError,
	}
	if path := fieldPath(err); path != "" {
		entry.Path = path
		if line, column, ok := c.locator.locate(path); ok {
			entry.Line = line
			entry.Column = column
		}
	}
	c.result.Valid = false
	c.result.Errors = append(c.result.Errors, entry)
}

// addPathError records a failure at a path the caller knows directly.
func (c *configValidationCollector) addPathError(field, path string, class engine.ConfigValidationErrorClass, err error) {
	c.add(field, class, &configFieldError{path: path, err: err})
}

// warn records a finding that does not block. Valid is untouched by design.
func (c *configValidationCollector) warn(field, path, message string) {
	entry := engine.ConfigValidationError{
		Field:    field,
		Message:  message,
		Class:    engine.ConfigErrorSchema,
		Severity: engine.ConfigSeverityWarning,
		Path:     path,
	}
	if line, column, ok := c.locator.locate(path); ok {
		entry.Line = line
		entry.Column = column
	}
	c.result.Warnings = append(c.result.Warnings, entry)
}
