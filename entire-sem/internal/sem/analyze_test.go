package sem

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeGitRange(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "auth.py", `def validate_token(token):
    return bool(token)
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "auth.py", `def validate_token(token, *, issuer=None):
    return bool(token)

def format_date(value):
    return str(value)
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "semantic change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("files = %#v", result.Files)
	}
	if len(result.Files[0].Changes) != 2 {
		t.Fatalf("changes = %#v", result.Files[0].Changes)
	}
}

func TestAnalyzeGitRangeDependentCounts(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "auth.py", `def validate_token(token):
    return bool(token)
`)
	write(t, repo, "use_auth.py", `def check(token):
    return validate_token(token)
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "auth.py", `def validate_token(token, *, issuer=None):
    return bool(token)
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "semantic change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range result.Files {
		for _, change := range file.Changes {
			if change.Name == "validate_token" && change.DependentsCount != 1 {
				t.Fatalf("dependents = %d, want 1 in %#v", change.DependentsCount, change)
			}
		}
	}
}

func TestAnalyzeGitRangeIgnoresPythonStringAndCommentDependents(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "auth.py", `def validate_token(token):
    return bool(token)
`)
	write(t, repo, "notes.py", `def mention_only():
    note = "validate_token is mentioned, not called"
    detail = '''validate_token in a doc string literal'''
    # validate_token appears in a comment too
    return note + detail
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "auth.py", `def validate_token(token, *, issuer=None):
    return bool(token)
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "semantic change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "validate_token")
	if change.DependentsCount != 0 {
		t.Fatalf("dependents = %d, want 0 for string/comment mentions in %#v", change.DependentsCount, change)
	}
}

func TestAnalyzeGitRangeIgnoresJavaScriptStringAndCommentDependents(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "api.js", `exports.run = (value) => value
`)
	write(t, repo, "notes.js", `function mentionOnly() {
  const text = "run is mentioned, not called"
  const template = `+"`run appears in a template literal`"+`
  const interpolation = `+"`${\"run is still a string\"} ${/* run in an expression comment */ 0}`"+`
  // run appears in a line comment
  /* run appears in a block comment */
  return text + template + interpolation
}
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "api.js", `exports.run = (value, strict = false) => value
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "semantic change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "exports.run")
	if change.DependentsCount != 0 {
		t.Fatalf("dependents = %d, want 0 for string/comment mentions in %#v", change.DependentsCount, change)
	}
}

func TestAnalyzeGitRangeCountsJavaScriptTemplateExpressionDependents(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "api.js", `exports.run = (value) => value
`)
	write(t, repo, "use.js", "function render(value) {\n  return `${run(value)}`\n}\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "api.js", `exports.run = (value, strict = false) => value
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "semantic change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "exports.run")
	if change.DependentsCount != 1 {
		t.Fatalf("dependents = %d, want render() in %#v", change.DependentsCount, change)
	}
}

func TestAnalyzeGitRangeIgnoresGoRawStringTemplateSyntaxDependents(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "main.go", "package main\n\nfunc Run(value string) string { return value }\n\nfunc MentionOnly() string {\n\treturn `${Run(\"value\")}`\n}\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "main.go", "package main\n\nfunc Run(value string, strict bool) string { return value }\n\nfunc MentionOnly() string {\n\treturn `${Run(\"value\")}`\n}\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "semantic change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "Run")
	if change.DependentsCount != 0 {
		t.Fatalf("dependents = %d, want 0 for Go raw string mention in %#v", change.DependentsCount, change)
	}
}

func TestAnalyzeGitRangeMultiLineSignatureChange(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "config.py", `def build_config(
    user,
):
    return {"user": user}
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "config.py", `def build_config(
    user,
    *,
    strict=False,
):
    return {"user": user}
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "multi-line signature change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "build_config")
	if change.Type != "signature_changed" {
		t.Fatalf("change type = %q, want signature_changed in %#v", change.Type, change)
	}
	if !strings.Contains(change.NewSignature, "strict=False") {
		t.Fatalf("new signature missing strict parameter: %#v", change)
	}
}

func TestAnalyzeGitRangePythonDecoratorSignatureChange(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "app.py", `def build(value):
    return value

def use(value):
    return build(value)
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "app.py", `@cache
def build(value):
    return value

def use(value):
    return build(value)
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "decorator signature change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "build")
	if change.Type != "signature_changed" {
		t.Fatalf("change type = %q, want signature_changed in %#v", change.Type, change)
	}
	if change.DependentsCount != 1 {
		t.Fatalf("dependents = %d, want use() in %#v", change.DependentsCount, change)
	}
	if !strings.Contains(change.NewSignature, "@cache") {
		t.Fatalf("new signature missing decorator: %#v", change)
	}
}

func TestAnalyzeGitRangeTypeScriptInterfaceMethodSignatureChange(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "app.ts", `interface Api {
  validate(value: string): boolean
}

function check(api: Api, value: string) { return api.validate(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "app.ts", `interface Api {
  validate(value: string, strict?: boolean): boolean
}

function check(api: Api, value: string) { return api.validate(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "interface signature change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "Api.validate")
	if change.Type != "signature_changed" {
		t.Fatalf("change type = %q, want signature_changed in %#v", change.Type, change)
	}
	if change.DependentsCount != 1 {
		t.Fatalf("dependents = %d, want check() in %#v", change.DependentsCount, change)
	}
	if !strings.Contains(change.NewSignature, "strict?: boolean") {
		t.Fatalf("new signature missing strict parameter: %#v", change)
	}
}

func TestAnalyzeGitRangeTypeScriptInterfaceFunctionPropertySignatureChange(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "app.ts", `interface Api {
  url: string
  validate: (value: string) => boolean
}

function check(api: Api, value: string) { return api.validate(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "app.ts", `interface Api {
  url: string
  validate: (value: string, strict?: boolean) => boolean
}

function check(api: Api, value: string) { return api.validate(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "function property signature change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "Api.validate")
	if change.Type != "signature_changed" {
		t.Fatalf("change type = %q, want signature_changed in %#v", change.Type, change)
	}
	if change.DependentsCount != 1 {
		t.Fatalf("dependents = %d, want check() in %#v", change.DependentsCount, change)
	}
	if !strings.Contains(change.NewSignature, "strict?: boolean") {
		t.Fatalf("new signature missing strict parameter: %#v", change)
	}
	if scalar := findChange(result, "Api.url"); scalar.Name != "" {
		t.Fatalf("non-function property produced change: %#v", scalar)
	}
}

func TestAnalyzeGitRangeTypeScriptTypeLiteralMethodSignatureChange(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "app.ts", `type Api = {
  validate(value: string): boolean
}

function check(api: Api, value: string) { return api.validate(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "app.ts", `type Api = {
  validate(value: string, strict?: boolean): boolean
}

function check(api: Api, value: string) { return api.validate(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "type literal signature change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "Api.validate")
	if change.Type != "signature_changed" {
		t.Fatalf("change type = %q, want signature_changed in %#v", change.Type, change)
	}
	if change.DependentsCount != 1 {
		t.Fatalf("dependents = %d, want check() in %#v", change.DependentsCount, change)
	}
	if !strings.Contains(change.NewSignature, "strict?: boolean") {
		t.Fatalf("new signature missing strict parameter: %#v", change)
	}
}

func TestAnalyzeGitRangeTypeScriptAccessorSignatureChange(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "app.ts", `class User {
  get name(): string { return "" }
  set name(value: string) {}
}

function render(user: User) { return user.name }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "app.ts", `class User {
  get name(): string | undefined { return "" }
  set name(value: string) {}
}

function render(user: User) { return user.name }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "getter signature change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChangeKind(t, result, "getter", "User.name")
	if change.Type != "signature_changed" {
		t.Fatalf("change type = %q, want signature_changed in %#v", change.Type, change)
	}
	if change.DependentsCount != 1 {
		t.Fatalf("dependents = %d, want render() in %#v", change.DependentsCount, change)
	}
	if !strings.Contains(change.NewSignature, "undefined") {
		t.Fatalf("new signature missing return union: %#v", change)
	}
}

func TestAnalyzeGitRangeTypeScriptMethodDecoratorSignatureChange(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "app.ts", `class User {
  save(value: string) { return value }
}

function render(user: User) { return user.save("x") }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "app.ts", `class User {
  @log
  save(value: string) { return value }
}

function render(user: User) { return user.save("x") }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "decorator signature change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "User.save")
	if change.Type != "signature_changed" {
		t.Fatalf("change type = %q, want signature_changed in %#v", change.Type, change)
	}
	if change.DependentsCount != 1 {
		t.Fatalf("dependents = %d, want render() in %#v", change.DependentsCount, change)
	}
	if !strings.Contains(change.NewSignature, "@log") {
		t.Fatalf("new signature missing decorator: %#v", change)
	}
}

func TestAnalyzeGitRangeJavaScriptAssignedFunctionSignatureChange(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "exports.js", `exports.run = (value) => value

function use(value) { return run(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "exports.js", `exports.run = (value, strict = false) => value

function use(value) { return run(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "assigned signature change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "exports.run")
	if change.Type != "signature_changed" {
		t.Fatalf("change type = %q, want signature_changed in %#v", change.Type, change)
	}
	if change.DependentsCount != 1 {
		t.Fatalf("dependents = %d, want use() in %#v", change.DependentsCount, change)
	}
	if !strings.Contains(change.NewSignature, "strict") {
		t.Fatalf("new signature missing strict parameter: %#v", change)
	}
}

func TestAnalyzeGitRangeJavaScriptDefaultExportBodyChange(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "main.js", `export default (value) => value + 1
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "main.js", `export default (value) => value + 2
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "default body change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "default")
	if change.Type != "body_changed" {
		t.Fatalf("change type = %q, want body_changed in %#v", change.Type, change)
	}
	if change.OldSignature != change.NewSignature {
		t.Fatalf("signatures differ: %#v", change)
	}
}

func TestAnalyzeGitRangeJavaScriptObjectFunctionSignatureChange(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "api.js", `const api = {
  save: (value) => value,
}

function use(value) { return api.save(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "api.js", `const api = {
  save: (value, strict = false) => value,
}

function use(value) { return api.save(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "object function signature change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "api.save")
	if change.Type != "signature_changed" {
		t.Fatalf("change type = %q, want signature_changed in %#v", change.Type, change)
	}
	if change.DependentsCount != 1 {
		t.Fatalf("dependents = %d, want use() in %#v", change.DependentsCount, change)
	}
	if !strings.Contains(change.NewSignature, "strict") {
		t.Fatalf("new signature missing strict parameter: %#v", change)
	}
}

func TestAnalyzeGitRangeJavaScriptDefaultObjectMethodBodyChange(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "main.js", `export default {
  render(value) { return value + 1 },
}
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "main.js", `export default {
  render(value) { return value + 2 },
}
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "default object body change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "default.render")
	if change.Type != "body_changed" {
		t.Fatalf("change type = %q, want body_changed in %#v", change.Type, change)
	}
	if change.OldSignature != change.NewSignature {
		t.Fatalf("signatures differ: %#v", change)
	}
}

func TestAnalyzeGitRangeIgnoresAnonymousObjectFunctionMembers(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "callback.js", `configure({
  run(value) { return value },
  save: (value) => value,
})
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "callback.js", `configure({
  run(value, strict = false) { return value },
  save: (value, strict = false) => value,
})
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "anonymous callback change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 0 {
		t.Fatalf("anonymous object function members should not produce semantic changes: %#v", result.Files)
	}
}

func TestAnalyzeGitRangeGoInterfaceMethodSignatureChange(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "main.go", `package main

type Reader interface { Read(p []byte) (n int, err error) }

func Use(r Reader, p []byte) { _, _ = r.Read(p) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "main.go", `package main

type Reader interface { Read(p []byte, strict bool) (n int, err error) }

func Use(r Reader, p []byte) { _, _ = r.Read(p) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "interface signature change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "Reader.Read")
	if change.Type != "signature_changed" {
		t.Fatalf("change type = %q, want signature_changed in %#v", change.Type, change)
	}
	if change.DependentsCount != 1 {
		t.Fatalf("dependents = %d, want Use() in %#v", change.DependentsCount, change)
	}
	if !strings.Contains(change.NewSignature, "strict bool") {
		t.Fatalf("new signature missing strict parameter: %#v", change)
	}
}

func TestAnalyzeGitRangeRustTraitImplScopesToImplementingType(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "lib.rs", `pub struct User;
pub struct Formatter;
pub struct Result;

impl std::fmt::Display for User {
    fn fmt(&self, f: &mut Formatter) -> Result { Result }
}

pub fn render(user: User, formatter: &mut Formatter) { user.fmt(formatter); }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "lib.rs", `pub struct User;
pub struct Formatter;
pub struct Result;

impl std::fmt::Display for User {
    fn fmt(&self, f: &mut Formatter, strict: bool) -> Result { Result }
}

pub fn render(user: User, formatter: &mut Formatter) { user.fmt(formatter); }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "trait impl signature change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "User.fmt")
	if change.Type != "signature_changed" {
		t.Fatalf("change type = %q, want signature_changed in %#v", change.Type, change)
	}
	if strings.Contains(change.Name, "Display") {
		t.Fatalf("trait impl method scoped to trait instead of implementing type: %#v", change)
	}
	if change.DependentsCount != 1 {
		t.Fatalf("dependents = %d, want render() in %#v", change.DependentsCount, change)
	}
	if !strings.Contains(change.NewSignature, "strict") {
		t.Fatalf("new signature missing strict parameter: %#v", change)
	}
}

func TestAnalyzeGitRangeArrowFunctionBodyChange(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "app.ts", `class User {
  save = (value: string) => value
}

const build = (value: number) => value + 1
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "app.ts", `class User {
  save = (value: string) => value.trim()
}

const build = (value: number) => value + 2
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "arrow body change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"build", "User.save"} {
		change := requireChange(t, result, name)
		if change.Type != "body_changed" {
			t.Fatalf("%s change type = %q, want body_changed in %#v", name, change.Type, change)
		}
		if change.OldSignature != change.NewSignature {
			t.Fatalf("%s signatures differ: %#v", name, change)
		}
	}
}

func TestAnalyzeGitRangeDoesNotCountContainingClassAsDependent(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "app.ts", `class User {
  static validate(value: string) { return Boolean(value) }
}

function checkUser(value: string) { return User.validate(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "app.ts", `class User {
  static validate(value: string, strict = false) { return Boolean(value) || strict }
}

function checkUser(value: string) { return User.validate(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "method signature change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "User.validate")
	if change.DependentsAmbiguous {
		t.Fatalf("dependents should not be ambiguous: %#v", change)
	}
	if change.DependentsCount != 1 {
		t.Fatalf("dependents = %d, want only checkUser in %#v", change.DependentsCount, change)
	}
}

func TestAnalyzeGitRangeMarksAmbiguousMethodDependents(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "main.go", `package main

type User struct{}
type Order struct{}

func (u User) Validate() bool { return true }
func (o Order) Validate() bool { return true }

func CheckUser(u User) bool { return u.Validate() }
func CheckOrder(o Order) bool { return o.Validate() }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "main.go", `package main

type User struct{}
type Order struct{}

func (u User) Validate(strict bool) bool { return strict }
func (o Order) Validate(strict bool) bool { return strict }

func CheckUser(u User) bool { return u.Validate() }
func CheckOrder(o Order) bool { return o.Validate() }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "ambiguous methods")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"User.Validate", "Order.Validate"} {
		change := requireChange(t, result, name)
		if !change.DependentsAmbiguous {
			t.Fatalf("%s should have ambiguous dependents: %#v", name, change)
		}
		if change.DependentsCount != 0 {
			t.Fatalf("%s dependents = %d, want conservative 0 for unqualified ambiguous calls", name, change.DependentsCount)
		}
	}
}

func TestAnalyzeGitRangeMarksUnchangedShortNameAsAmbiguous(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "app.ts", `class User { static validate(value: string) { return Boolean(value) } }
class Order { static validate(value: string) { return Boolean(value) } }

function checkUser(value: string) { return User.validate(value) }
function checkLoose(value: string) { return validate(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "app.ts", `class User { static validate(value: string, strict = false) { return Boolean(value) || strict } }
class Order { static validate(value: string) { return Boolean(value) } }

function checkUser(value: string) { return User.validate(value) }
function checkLoose(value: string) { return validate(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "one method signature change")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "User.validate")
	if !change.DependentsAmbiguous {
		t.Fatalf("dependents should be ambiguous because Order.validate still exists: %#v", change)
	}
	if change.DependentsCount != 1 {
		t.Fatalf("dependents = %d, want only qualified checkUser in %#v", change.DependentsCount, change)
	}
}

func TestAnalyzeGitRangeMarksRemovedShortNameAsAmbiguous(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "app.ts", `class User { static validate(value: string) { return Boolean(value) } }
class Order { static validate(value: string) { return Boolean(value) } }

function checkUser(value: string) { return User.validate(value) }
function checkLoose(value: string) { return validate(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "app.ts", `class User {}
class Order { static validate(value: string) { return Boolean(value) } }

function checkUser(value: string) { return User.validate(value) }
function checkLoose(value: string) { return validate(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "remove one method")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	change := requireChange(t, result, "User.validate")
	if change.Type != "removed" {
		t.Fatalf("change type = %q, want removed in %#v", change.Type, change)
	}
	if !change.DependentsAmbiguous {
		t.Fatalf("dependents should be ambiguous because Order.validate still exists: %#v", change)
	}
	if change.DependentsCount != 1 {
		t.Fatalf("dependents = %d, want only qualified checkUser in %#v", change.DependentsCount, change)
	}
}

func TestAnalyzeGitRangeCountsQualifiedAmbiguousDependents(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "app.ts", `class User { static validate(value: string) { return Boolean(value) } }
class Order { static validate(value: string) { return Boolean(value) } }

function checkUser(value: string) { return User.validate(value) }
function checkOrder(value: string) { return Order.validate(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	base := rev(t, repo, "HEAD")

	write(t, repo, "app.ts", `class User { static validate(value: string, strict = false) { return Boolean(value) || strict } }
class Order { static validate(value: string, strict = false) { return Boolean(value) || strict } }

function checkUser(value: string) { return User.validate(value) }
function checkOrder(value: string) { return Order.validate(value) }
`)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "qualified ambiguous methods")
	head := rev(t, repo, "HEAD")

	result, err := AnalyzeGitRange(context.Background(), repo, base, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"User.validate", "Order.validate"} {
		change := requireChange(t, result, name)
		if !change.DependentsAmbiguous {
			t.Fatalf("%s should have ambiguous dependents: %#v", name, change)
		}
		if change.DependentsCount != 1 {
			t.Fatalf("%s dependents = %d, want 1 qualified dependent", name, change.DependentsCount)
		}
	}
}

func TestAnalyzeCheckpointResolvesAssociatedCommit(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.name", "Entire Sem Test")
	git(t, repo, "config", "user.email", "sem@example.com")

	write(t, repo, "auth.py", "def validate_token(token):\n    return bool(token)\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")

	write(t, repo, "auth.py", "def validate_token(token, issuer=None):\n    return bool(token)\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "agent update\n\nEntire-Checkpoint: abc123def456")

	result, err := AnalyzeCheckpoint(context.Background(), repo, "abc123def456")
	if err != nil {
		t.Fatal(err)
	}
	if result.Checkpoint != "abc123def456" {
		t.Fatalf("checkpoint = %q", result.Checkpoint)
	}
	if len(result.Files) != 1 {
		t.Fatalf("files = %#v", result.Files)
	}
}

func requireChange(t *testing.T, result Result, name string) EntityChange {
	t.Helper()
	if change := findChange(result, name); change.Name != "" || change.NewName != "" {
		return change
	}
	t.Fatalf("missing change %q in %#v", name, result.Files)
	return EntityChange{}
}

func findChange(result Result, name string) EntityChange {
	for _, file := range result.Files {
		for _, change := range file.Changes {
			if change.Name == name || change.NewName == name {
				return change
			}
		}
	}
	return EntityChange{}
}

func requireChangeKind(t *testing.T, result Result, kind, name string) EntityChange {
	t.Helper()
	for _, file := range result.Files {
		for _, change := range file.Changes {
			if change.Kind == kind && (change.Name == name || change.NewName == name) {
				return change
			}
		}
	}
	t.Fatalf("missing %s change %q in %#v", kind, name, result.Files)
	return EntityChange{}
}

func write(t *testing.T, repo, path, content string) {
	t.Helper()
	full := filepath.Join(repo, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func rev(t *testing.T, repo, value string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", value)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v\n%s", value, err, out)
	}
	return string(out[:len(out)-1])
}
