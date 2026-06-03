package sem

import "testing"

func TestTreeSitterParserPythonEntities(t *testing.T) {
	input := `class Token:
    pass

def validate_token(token: str) -> bool:
    return bool(token)

async def refresh_token(token):
    return token
`
	entities, language := TreeSitterParser{}.Parse("auth.py", input)
	if language != "Python" {
		t.Fatalf("language = %q", language)
	}
	if len(entities) != 3 {
		t.Fatalf("entities = %#v", entities)
	}
	if entities[0].Kind != "class" || entities[0].Name != "Token" {
		t.Fatalf("first entity = %#v", entities[0])
	}
	if entities[1].Kind != "function" || entities[1].Name != "validate_token" {
		t.Fatalf("second entity = %#v", entities[1])
	}
	if entities[2].Kind != "function" || entities[2].Name != "refresh_token" {
		t.Fatalf("third entity = %#v", entities[2])
	}
}

func TestCompareSignatureBodyRenameAddRemove(t *testing.T) {
	before, _ := TreeSitterParser{}.Parse("auth.py", `def validate_token(token):
    return bool(token)

def old_name(value):
    return value + 1

def removed():
    return False
`)
	after, _ := TreeSitterParser{}.Parse("auth.py", `def validate_token(token, *, issuer=None):
    return bool(token)

def new_name(value):
    return value + 1

def added():
    return True
`)
	changes := Compare(before, after)
	seen := map[string]bool{}
	for _, change := range changes {
		seen[change.Type+":"+change.Name] = true
		if change.Type == "renamed" {
			seen["renamed:"+change.OldName+"->"+change.NewName] = true
		}
	}
	for _, want := range []string{
		"signature_changed:validate_token",
		"renamed:old_name->new_name",
		"removed:removed",
		"added:added",
	} {
		if !seen[want] {
			t.Fatalf("missing %s in %#v", want, changes)
		}
	}
}

func TestTreeSitterParserDoesNotScopeLocalFunctionsAsMethods(t *testing.T) {
	entities, language := TreeSitterParser{}.Parse("worker.py", `class Runner:
    def run(self):
        def helper():
            return True
        return helper()
`)
	if language != "Python" {
		t.Fatalf("language = %q", language)
	}
	seen := map[string]string{}
	for _, entity := range entities {
		seen[entity.Name] = entity.Kind
	}
	if seen["Runner"] != "class" {
		t.Fatalf("missing Runner class in %#v", entities)
	}
	if seen["Runner.run"] != "method" {
		t.Fatalf("missing Runner.run method in %#v", entities)
	}
	if seen["helper"] != "function" {
		t.Fatalf("missing local helper function in %#v", entities)
	}
	if _, ok := seen["Runner.helper"]; ok {
		t.Fatalf("local helper was incorrectly scoped as Runner.helper in %#v", entities)
	}
}

func TestTreeSitterParserTypeScriptAccessorsAndPrivateMembers(t *testing.T) {
	entities, language := TreeSitterParser{}.Parse("user.ts", `class User {
  get name(): string { return "" }
  set name(value: string) {}
  #secret(value: string) { return value }
  #save = (value: string) => value
}
`)
	if language != "TypeScript" {
		t.Fatalf("language = %q", language)
	}
	seen := map[string]bool{}
	for _, entity := range entities {
		seen[entity.Kind+":"+entity.Name] = true
	}
	for _, want := range []string{
		"class:User",
		"getter:User.name",
		"setter:User.name",
		"method:User.#secret",
		"method:User.#save",
	} {
		if !seen[want] {
			t.Fatalf("missing %s in %#v", want, entities)
		}
	}
}

func TestTreeSitterParserMultiLanguageEntities(t *testing.T) {
	tests := []struct {
		path     string
		input    string
		language string
		names    []string
	}{
		{
			path:     "main.go",
			language: "Go",
			input: `package main
type User struct { Name string }
type Store[T any] struct { Value T }
type Reader interface { Read(p []byte) (n int, err error) }
func (u User) Validate(value string) bool { return value != "" }
func (s *Store[T]) Load() T { return s.Value }
func Format() {}
`,
			names: []string{"User", "Store", "Reader", "Reader.Read", "User.Validate", "Store.Load", "Format"},
		},
		{
			path:     "app.ts",
			language: "TypeScript",
			input: `interface Foo { value: string; validate(value: string): boolean }
type Bar = string
type Contract = { parse(input: string): number }
abstract class Base { abstract run(value: string): boolean }
class User {
  validate(value: string) { return value }
  save = (value: string) => value
}
const build = (value: number) => value + 1
`,
			names: []string{"Foo", "Foo.validate", "Bar", "Contract", "Contract.parse", "Base", "Base.run", "User", "User.validate", "User.save", "build"},
		},
		{
			path:     "lib.rs",
			language: "Rust",
			input: `pub struct User { name: String }
pub struct Bag<T>(T);
pub fn validate(value: &str) -> bool { true }
trait Run { fn run(&self); }
impl User { pub fn active(&self) -> bool { true } }
impl<T> Bag<T> { pub fn unwrap_owned(self) -> T { self.0 } }
`,
			names: []string{"User", "Bag", "validate", "Run", "Run.run", "User.active", "Bag.unwrap_owned"},
		},
	}

	for _, tt := range tests {
		entities, language := TreeSitterParser{}.Parse(tt.path, tt.input)
		if language != tt.language {
			t.Fatalf("%s language = %q", tt.path, language)
		}
		seen := map[string]bool{}
		for _, entity := range entities {
			seen[entity.Name] = true
		}
		for _, name := range tt.names {
			if !seen[name] {
				t.Fatalf("%s missing entity %q in %#v", tt.path, name, entities)
			}
		}
	}
}
