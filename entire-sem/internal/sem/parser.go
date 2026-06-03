package sem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	treesittertsx "github.com/smacker/go-tree-sitter/typescript/tsx"
	treesitterts "github.com/smacker/go-tree-sitter/typescript/typescript"
)

type languageSpec struct {
	language string
	grammar  *sitter.Language
}

var treeSitterLanguages = map[string]languageSpec{
	".go":  {language: "Go", grammar: golang.GetLanguage()},
	".py":  {language: "Python", grammar: python.GetLanguage()},
	".js":  {language: "JavaScript", grammar: javascript.GetLanguage()},
	".jsx": {language: "JavaScript", grammar: treesittertsx.GetLanguage()},
	".ts":  {language: "TypeScript", grammar: treesitterts.GetLanguage()},
	".tsx": {language: "TypeScript", grammar: treesittertsx.GetLanguage()},
	".rs":  {language: "Rust", grammar: rust.GetLanguage()},
}

type TreeSitterParser struct{}

func (TreeSitterParser) Parse(path, content string) ([]Entity, string) {
	spec, ok := languageForPath(path)
	if !ok {
		return nil, ""
	}
	src := []byte(content)
	root, err := sitter.ParseCtx(context.Background(), src, spec.grammar)
	if err != nil || root == nil || root.IsNull() {
		return nil, spec.language
	}

	var entities []Entity
	walkEntities(root, src, "", &entities)
	sort.Slice(entities, func(i, j int) bool {
		if entities[i].StartLine == entities[j].StartLine {
			return entities[i].Name < entities[j].Name
		}
		return entities[i].StartLine < entities[j].StartLine
	})
	return entities, spec.language
}

func Supported(path string) bool {
	_, ok := languageForPath(path)
	return ok
}

func languageForPath(path string) (languageSpec, bool) {
	spec, ok := treeSitterLanguages[strings.ToLower(filepath.Ext(path))]
	return spec, ok
}

func walkEntities(node *sitter.Node, src []byte, scope string, entities *[]Entity) {
	if !validNode(node) {
		return
	}
	entity, ok := entityFromNode(node, src, scope)
	childScope := scope
	if ok {
		*entities = append(*entities, entity)
		if scopesChildren(entity.Kind) {
			childScope = entity.Name
		} else {
			childScope = ""
		}
	} else if nextScope := scopeFromNode(node, src, scope); nextScope != "" {
		childScope = nextScope
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		walkEntities(node.NamedChild(i), src, childScope, entities)
	}
}

func entityFromNode(node *sitter.Node, src []byte, scope string) (Entity, bool) {
	var kind string
	var name string
	switch node.Type() {
	case "class_definition", "class_declaration", "abstract_class_declaration":
		kind = "class"
		name = nodeName(node, src)
	case "function_definition":
		kind = "function"
		name = nodeName(node, src)
		if scope != "" {
			kind = "method"
			name = qualify(scope, name)
		}
	case "function_declaration", "function_item":
		kind = "function"
		name = nodeName(node, src)
		if scope != "" {
			kind = "method"
			name = qualify(scope, name)
		}
	case "function_signature_item":
		kind = "function"
		name = nodeName(node, src)
		if scope != "" {
			kind = "method"
			name = qualify(scope, name)
		}
	case "method_declaration":
		kind = "method"
		name = nodeName(node, src)
		if receiver := goReceiverName(node, src); receiver != "" {
			name = qualify(receiver, name)
		}
	case "method_definition":
		if scope == "" {
			return Entity{}, false
		}
		kind = methodKind(node, src)
		name = nodeName(node, src)
		name = qualify(scope, name)
	case "method_signature", "abstract_method_signature", "method_elem":
		if scope == "" {
			return Entity{}, false
		}
		kind = methodKind(node, src)
		name = nodeName(node, src)
		name = qualify(scope, name)
	case "property_signature":
		if scope == "" || !typeSignatureFunctionLike(node) {
			return Entity{}, false
		}
		kind = "method"
		name = qualify(scope, nodeName(node, src))
	case "type_spec", "type_alias_declaration":
		kind = "type"
		name = nodeName(node, src)
		if hasNamedChildType(node, "interface_type") || hasNamedChildType(node, "object_type") {
			kind = "interface"
		}
	case "interface_declaration":
		kind = "interface"
		name = nodeName(node, src)
	case "struct_item":
		kind = "struct"
		name = nodeName(node, src)
	case "enum_item":
		kind = "enum"
		name = nodeName(node, src)
	case "trait_item":
		kind = "trait"
		name = nodeName(node, src)
	case "variable_declarator":
		value := node.ChildByFieldName("value")
		if !functionLikeValue(value) {
			return Entity{}, false
		}
		kind = "function"
		name = nodeName(node, src)
	case "assignment":
		value := assignmentValue(node)
		if !functionLikeValue(value) {
			return Entity{}, false
		}
		name = referenceName(assignmentTarget(node), src)
		kind = "function"
		if scope != "" {
			kind = "method"
			name = qualify(scope, name)
		}
	case "assignment_expression":
		value := assignmentValue(node)
		if !functionLikeValue(value) {
			return Entity{}, false
		}
		name = referenceName(assignmentTarget(node), src)
		kind = assignedFunctionKind(name)
	case "pair":
		if scope == "" {
			return Entity{}, false
		}
		value := objectPairValue(node)
		if !functionLikeValue(value) {
			return Entity{}, false
		}
		kind = "method"
		name = qualify(scope, nodeName(node, src))
	case "field_definition", "public_field_definition":
		value := node.ChildByFieldName("value")
		if !functionLikeValue(value) {
			return Entity{}, false
		}
		kind = "function"
		name = nodeName(node, src)
		if scope != "" {
			kind = "method"
			name = qualify(scope, name)
		}
	case "export_statement":
		if !functionLikeValue(exportDefaultFunctionValue(node, src)) {
			return Entity{}, false
		}
		kind = "function"
		name = "default"
	default:
		return Entity{}, false
	}
	if name == "" {
		return Entity{}, false
	}

	block := node.Content(src)
	entity := Entity{
		Kind:        kind,
		Name:        name,
		Signature:   signatureFromNode(node, src),
		StartLine:   int(node.StartPoint().Row) + 1,
		EndLine:     int(node.EndPoint().Row) + 1,
		BodyHash:    hash(normalize(block)),
		Fingerprint: hash(normalize(entityFingerprintSource(Entity{Name: name, Signature: signatureFromNode(node, src)}, block))),
	}
	return entity, true
}

func nodeName(node *sitter.Node, src []byte) string {
	for _, field := range []string{"name", "property", "type"} {
		child := node.ChildByFieldName(field)
		if validNode(child) {
			return child.Content(src)
		}
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if validNode(child) && isNameNode(child.Type()) {
			return child.Content(src)
		}
	}
	return ""
}

func assignmentTarget(node *sitter.Node) *sitter.Node {
	if target := node.ChildByFieldName("left"); validNode(target) {
		return target
	}
	if node.NamedChildCount() > 0 {
		return node.NamedChild(0)
	}
	return nil
}

func assignmentValue(node *sitter.Node) *sitter.Node {
	if value := node.ChildByFieldName("right"); validNode(value) {
		return value
	}
	if node.NamedChildCount() > 1 {
		return node.NamedChild(1)
	}
	return nil
}

func objectPairValue(node *sitter.Node) *sitter.Node {
	if value := node.ChildByFieldName("value"); validNode(value) {
		return value
	}
	if node.NamedChildCount() > 1 {
		return node.NamedChild(1)
	}
	return nil
}

func assignedFunctionKind(name string) string {
	if strings.Contains(name, ".") && name != "module.exports" && !strings.HasPrefix(name, "module.exports.") && !strings.HasPrefix(name, "exports.") {
		return "method"
	}
	return "function"
}

func referenceName(node *sitter.Node, src []byte) string {
	if !validNode(node) {
		return ""
	}
	if isNameNode(node.Type()) {
		return node.Content(src)
	}
	if node.Type() != "member_expression" {
		return ""
	}
	parts := make([]string, 0, node.NamedChildCount())
	for i := 0; i < int(node.NamedChildCount()); i++ {
		part := referenceName(node.NamedChild(i), src)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ".")
}

func exportDefaultFunctionValue(node *sitter.Node, src []byte) *sitter.Node {
	if !strings.HasPrefix(normalize(node.Content(src)), "export default ") {
		return nil
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if functionLikeValue(child) {
			return child
		}
	}
	return nil
}

func exportDefaultObjectValue(node *sitter.Node, src []byte) *sitter.Node {
	if !strings.HasPrefix(normalize(node.Content(src)), "export default ") {
		return nil
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if objectLikeValue(child) {
			return child
		}
	}
	return nil
}

func hasNamedChildType(node *sitter.Node, nodeType string) bool {
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if validNode(child) && child.Type() == nodeType {
			return true
		}
	}
	return false
}

func hasNamedDescendantType(node *sitter.Node, nodeTypes ...string) bool {
	if !validNode(node) {
		return false
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if !validNode(child) {
			continue
		}
		for _, nodeType := range nodeTypes {
			if child.Type() == nodeType {
				return true
			}
		}
		if hasNamedDescendantType(child, nodeTypes...) {
			return true
		}
	}
	return false
}

func isNameNode(nodeType string) bool {
	switch nodeType {
	case "identifier", "type_identifier", "field_identifier", "property_identifier", "private_property_identifier", "package_identifier":
		return true
	default:
		return false
	}
}

func methodKind(node *sitter.Node, src []byte) string {
	for _, token := range strings.Fields(signatureFromNode(node, src)) {
		switch token {
		case "get":
			return "getter"
		case "set":
			return "setter"
		}
	}
	return "method"
}

func signatureFromNode(node *sitter.Node, src []byte) string {
	start := signatureStartByte(node)
	end := node.EndByte()
	if value := functionValueForSignature(node, src); functionLikeValue(value) {
		if bodyStart, ok := functionBodyStart(value); ok {
			end = bodyStart
		}
	} else if bodyStart, ok := functionBodyStart(node); ok {
		end = bodyStart
	}
	if end <= start || int(end) > len(src) {
		end = node.EndByte()
	}
	return normalizeSignature(string(src[start:end]))
}

func functionValueForSignature(node *sitter.Node, src []byte) *sitter.Node {
	if value := node.ChildByFieldName("value"); functionLikeValue(value) {
		return value
	}
	if value := assignmentValue(node); functionLikeValue(value) {
		return value
	}
	if value := exportDefaultFunctionValue(node, src); functionLikeValue(value) {
		return value
	}
	return nil
}

func signatureStartByte(node *sitter.Node) uint32 {
	start := node.StartByte()
	for prev := node.PrevNamedSibling(); validNode(prev) && prev.Type() == "decorator"; prev = prev.PrevNamedSibling() {
		start = prev.StartByte()
	}
	return start
}

func functionBodyStart(node *sitter.Node) (uint32, bool) {
	if body := node.ChildByFieldName("body"); validNode(body) {
		return body.StartByte(), true
	}
	if body := firstBodyLikeChild(node); validNode(body) {
		return body.StartByte(), true
	}
	return 0, false
}

func firstBodyLikeChild(node *sitter.Node) *sitter.Node {
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if !validNode(child) {
			continue
		}
		switch child.Type() {
		case "block", "statement_block", "class_body", "declaration_list", "field_declaration_list", "interface_body":
			return child
		}
	}
	return nil
}

func functionLikeValue(node *sitter.Node) bool {
	if !validNode(node) {
		return false
	}
	switch node.Type() {
	case "arrow_function", "function", "function_expression", "generator_function", "lambda":
		return true
	default:
		return false
	}
}

func objectLikeValue(node *sitter.Node) bool {
	return validNode(node) && node.Type() == "object"
}

func typeSignatureFunctionLike(node *sitter.Node) bool {
	return hasNamedDescendantType(node, "function_type", "constructor_type", "call_signature")
}

func scopesChildren(kind string) bool {
	switch kind {
	case "class", "struct", "trait", "interface":
		return true
	default:
		return false
	}
}

func scopeFromNode(node *sitter.Node, src []byte, parentScope string) string {
	switch node.Type() {
	case "impl_item":
		return qualify(parentScope, rustImplName(node, src))
	case "variable_declarator":
		if objectLikeValue(node.ChildByFieldName("value")) {
			return qualify(parentScope, nodeName(node, src))
		}
	case "assignment_expression":
		if objectLikeValue(assignmentValue(node)) {
			return qualify(parentScope, referenceName(assignmentTarget(node), src))
		}
	case "export_statement":
		if objectLikeValue(exportDefaultObjectValue(node, src)) {
			return qualify(parentScope, "default")
		}
	case "pair":
		if scopeName := qualify(parentScope, nodeName(node, src)); parentScope != "" && objectLikeValue(objectPairValue(node)) {
			return scopeName
		}
	default:
	}
	return ""
}

func rustImplName(node *sitter.Node, src []byte) string {
	if target := node.ChildByFieldName("type"); validNode(target) {
		return normalizeRustTypeName(target.Content(src))
	}
	var target string
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if !validNode(child) {
			continue
		}
		switch child.Type() {
		case "declaration_list":
			return target
		case "type_identifier", "generic_type", "scoped_type_identifier":
			target = normalizeRustTypeName(child.Content(src))
		}
	}
	return target
}

func normalizeRustTypeName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if index := strings.Index(value, "<"); index >= 0 {
		value = value[:index]
	}
	if index := strings.LastIndex(value, "::"); index >= 0 {
		value = value[index+2:]
	}
	value = strings.Trim(value, "&*[]() \t\r\n")
	return value
}

func goReceiverName(node *sitter.Node, src []byte) string {
	signature := signatureFromNode(node, src)
	receiverStart := strings.Index(signature, "func (")
	if receiverStart < 0 {
		return ""
	}
	receiver := signature[receiverStart+len("func ("):]
	receiverEnd := strings.Index(receiver, ")")
	if receiverEnd < 0 {
		return ""
	}
	receiver = strings.TrimSpace(receiver[:receiverEnd])
	fields := strings.Fields(receiver)
	if len(fields) == 0 {
		return ""
	}
	return normalizeGoReceiverTypeName(fields[len(fields)-1])
}

func qualify(scope, name string) string {
	if scope == "" || name == "" || strings.HasPrefix(name, scope+".") {
		return name
	}
	return scope + "." + name
}

func normalizeGoReceiverTypeName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(value, "*[]")
	value = strings.TrimRight(value, " \t\r\n")
	if index := strings.Index(value, "["); index >= 0 {
		value = value[:index]
	}
	value = strings.TrimRight(value, "*[]")
	if index := strings.LastIndex(value, "."); index >= 0 {
		value = value[index+1:]
	}
	return strings.TrimSpace(value)
}

func validNode(node *sitter.Node) bool {
	return node != nil && !node.IsNull()
}

func normalize(value string) string {
	fields := strings.Fields(value)
	return strings.Join(fields, " ")
}

func normalizeSignature(value string) string {
	return strings.TrimSpace(strings.TrimRight(normalize(value), "{:; \t\r\n"))
}

func entityFingerprintSource(entity Entity, block string) string {
	lines := strings.Split(block, "\n")
	if len(lines) <= 1 {
		return strings.ReplaceAll(entity.Signature, entity.Name, "<name>")
	}
	return strings.Join(lines[1:], "\n")
}

func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}
