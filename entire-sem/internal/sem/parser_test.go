package sem

import (
	"strings"
	"testing"
)

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

func TestTreeSitterParserIncludesDecoratorsInSignatures(t *testing.T) {
	pythonEntities, language := TreeSitterParser{}.Parse("models.py", `@dataclass
class User:
    @classmethod
    def from_id(cls, value):
        return cls()

@cache
def build(value):
    return value
`)
	if language != "Python" {
		t.Fatalf("language = %q", language)
	}
	assertEntitySignatureContains(t, pythonEntities, "User", "@dataclass")
	assertEntitySignatureContains(t, pythonEntities, "User.from_id", "@classmethod")
	assertEntitySignatureContains(t, pythonEntities, "build", "@cache")

	tsEntities, language := TreeSitterParser{}.Parse("user.ts", `class User {
  @log
  save(value: string) { return value }
}
`)
	if language != "TypeScript" {
		t.Fatalf("language = %q", language)
	}
	assertEntitySignatureContains(t, tsEntities, "User.save", "@log")
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

func assertEntitySignatureContains(t *testing.T, entities []Entity, name, want string) {
	t.Helper()
	for _, entity := range entities {
		if entity.Name != name {
			continue
		}
		if !strings.Contains(entity.Signature, want) {
			t.Fatalf("%s signature = %q, want %q in %#v", name, entity.Signature, want, entity)
		}
		return
	}
	t.Fatalf("missing entity %q in %#v", name, entities)
}

func entityKind(entities []Entity, name string) string {
	for _, entity := range entities {
		if entity.Name == name {
			return entity.Kind
		}
	}
	return ""
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

func TestTreeSitterParserPythonAssignedLambdaEntities(t *testing.T) {
	entities, language := TreeSitterParser{}.Parse("pipeline.py", `validate_token = lambda token: bool(token)

class Pipeline:
    normalize = lambda self, value: value.strip()
`)
	if language != "Python" {
		t.Fatalf("language = %q", language)
	}
	seen := map[string]string{}
	for _, entity := range entities {
		seen[entity.Name] = entity.Kind
	}
	for name, kind := range map[string]string{
		"validate_token":     "function",
		"Pipeline":           "class",
		"Pipeline.normalize": "method",
	} {
		if seen[name] != kind {
			t.Fatalf("%s kind = %q, want %q in %#v", name, seen[name], kind, entities)
		}
	}
	assertEntitySignatureContains(t, entities, "validate_token", "lambda token")
	assertEntitySignatureContains(t, entities, "Pipeline.normalize", "lambda self, value")
}

func TestTreeSitterParserGoAssignedFunctionEntities(t *testing.T) {
	entities, language := TreeSitterParser{}.Parse("main.go", `package main

var Validate = func(value string) bool { return value != "" }
var Label = "not a function"
`)
	if language != "Go" {
		t.Fatalf("language = %q", language)
	}
	seen := map[string]string{}
	for _, entity := range entities {
		seen[entity.Name] = entity.Kind
	}
	if seen["Validate"] != "function" {
		t.Fatalf("Validate kind = %q, want function in %#v", seen["Validate"], entities)
	}
	if _, ok := seen["Label"]; ok {
		t.Fatalf("non-function variable leaked as entity in %#v", entities)
	}
	assertEntitySignatureContains(t, entities, "Validate", "func(value string) bool")
}

func TestTreeSitterParserTypeScriptAccessorsAndPrivateMembers(t *testing.T) {
	entities, language := TreeSitterParser{}.Parse("user.ts", `class User {
  constructor(id: string) {}
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
		"constructor:User.constructor",
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

func TestTreeSitterParserTypeScriptMemberModifiersInSignatures(t *testing.T) {
	entities, language := TreeSitterParser{}.Parse("user.ts", `abstract class Base {
  protected abstract run(value: string): string
}

class User {
  public static validate(value: string) { return Boolean(value) }
  private save(value: string) { return value }
  readonly build = (value: string) => value
}
`)
	if language != "TypeScript" {
		t.Fatalf("language = %q", language)
	}
	for name, want := range map[string]string{
		"Base.run":      "protected abstract run",
		"User.validate": "public static validate",
		"User.save":     "private save",
		"User.build":    "readonly build",
	} {
		assertEntitySignatureContains(t, entities, name, want)
	}
}

func TestTreeSitterParserJavaScriptAssignedAndDefaultFunctions(t *testing.T) {
	entities, language := TreeSitterParser{}.Parse("exports.js", `module.exports = function(value) { return value }
exports.run = (value) => value
Foo.build = function(value) { return value }
function* stream(value) { yield value }
export default (value) => value
`)
	if language != "JavaScript" {
		t.Fatalf("language = %q", language)
	}
	seen := map[string]string{}
	for _, entity := range entities {
		seen[entity.Name] = entity.Kind
	}
	for name, kind := range map[string]string{
		"module.exports": "function",
		"exports.run":    "function",
		"Foo.build":      "method",
		"stream":         "function",
		"default":        "function",
	} {
		if seen[name] != kind {
			t.Fatalf("%s kind = %q, want %q in %#v", name, seen[name], kind, entities)
		}
	}
	assertEntitySignatureContains(t, entities, "default", "export default")
}

func TestTreeSitterParserJavaScriptDefaultFunctionDeclarations(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		input         string
		language      string
		localName     string
		signatureWant string
	}{
		{
			name:          "javascript function",
			path:          "default.js",
			input:         `export default function render(value) { return value }`,
			language:      "JavaScript",
			localName:     "render",
			signatureWant: "export default function render",
		},
		{
			name:          "javascript async function",
			path:          "default.js",
			input:         `export default async function load(value) { return value }`,
			language:      "JavaScript",
			localName:     "load",
			signatureWant: "export default async function load",
		},
		{
			name:          "javascript generator function",
			path:          "default.js",
			input:         `export default function* stream(value) { yield value }`,
			language:      "JavaScript",
			localName:     "stream",
			signatureWant: "export default function* stream",
		},
		{
			name:          "typescript function",
			path:          "default.ts",
			input:         `export default function render(value: string): string { return value }`,
			language:      "TypeScript",
			localName:     "render",
			signatureWant: "export default function render",
		},
		{
			name:          "anonymous function",
			path:          "default.js",
			input:         `export default function(value) { return value }`,
			language:      "JavaScript",
			localName:     "",
			signatureWant: "export default function",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entities, language := TreeSitterParser{}.Parse(tt.path, tt.input+"\n")
			if language != tt.language {
				t.Fatalf("language = %q", language)
			}
			seen := map[string]string{}
			for _, entity := range entities {
				seen[entity.Name] = entity.Kind
			}
			if seen["default"] != "function" {
				t.Fatalf("default kind = %q, want function in %#v", seen["default"], entities)
			}
			if tt.localName != "" {
				if _, ok := seen[tt.localName]; ok {
					t.Fatalf("named default export leaked local function entity in %#v", entities)
				}
			}
			assertEntitySignatureContains(t, entities, "default", tt.signatureWant)
		})
	}
}

func TestTreeSitterParserDefaultClassDeclarations(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		input        string
		language     string
		localName    string
		methodName   string
		methodKind   string
		signatureTag string
	}{
		{
			name:         "javascript class",
			path:         "view.js",
			input:        `export default class View { render(value) { return value } }`,
			language:     "JavaScript",
			localName:    "View",
			methodName:   "default.render",
			methodKind:   "method",
			signatureTag: "export default class View",
		},
		{
			name:         "typescript abstract class",
			path:         "base.ts",
			input:        `export default abstract class Base { abstract run(value: string): string }`,
			language:     "TypeScript",
			localName:    "Base",
			methodName:   "default.run",
			methodKind:   "method",
			signatureTag: "export default abstract class Base",
		},
		{
			name:         "anonymous javascript class",
			path:         "view.js",
			input:        `export default class { render(value) { return value } }`,
			language:     "JavaScript",
			localName:    "",
			methodName:   "default.render",
			methodKind:   "method",
			signatureTag: "export default class",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entities, language := TreeSitterParser{}.Parse(tt.path, tt.input+"\n")
			if language != tt.language {
				t.Fatalf("language = %q", language)
			}
			seen := map[string]string{}
			for _, entity := range entities {
				seen[entity.Name] = entity.Kind
			}
			if seen["default"] != "class" {
				t.Fatalf("default kind = %q, want class in %#v", seen["default"], entities)
			}
			if seen[tt.methodName] != tt.methodKind {
				t.Fatalf("%s kind = %q, want %q in %#v", tt.methodName, seen[tt.methodName], tt.methodKind, entities)
			}
			if tt.localName != "" {
				if _, ok := seen[tt.localName]; ok {
					t.Fatalf("named default export leaked local class entity in %#v", entities)
				}
			}
			assertEntitySignatureContains(t, entities, "default", tt.signatureTag)
		})
	}
}

func TestTreeSitterParserIncludesNamedExportModifiersInSignatures(t *testing.T) {
	entities, language := TreeSitterParser{}.Parse("exports.ts", `export function run(value: string): string { return value }
export async function load(value: string): Promise<string> { return Promise.resolve(value) }
export class View { render(value: string) { return value } }
export interface Api { request(path: string): string }
export type Handler = (value: string) => string
export const build = (value: string) => value
`)
	if language != "TypeScript" {
		t.Fatalf("language = %q", language)
	}
	for name, want := range map[string]string{
		"run":     "export function run",
		"load":    "export async function load",
		"View":    "export class View",
		"Api":     "export interface Api",
		"Handler": "export type Handler",
		"build":   "export const build",
	} {
		assertEntitySignatureContains(t, entities, name, want)
	}
}

func TestTreeSitterParserTypeScriptAmbientDeclarations(t *testing.T) {
	entities, language := TreeSitterParser{}.Parse("api.d.ts", `declare function run(value: string): string
declare class View { render(value: string): string }
declare interface Api { request(path: string): string }
export declare const build: (value: string) => string
export declare const config: { build(value: string): string }
`)
	if language != "TypeScript" {
		t.Fatalf("language = %q", language)
	}
	for name, kind := range map[string]string{
		"run":         "function",
		"View":        "class",
		"View.render": "method",
		"Api":         "interface",
		"Api.request": "method",
		"build":       "function",
	} {
		if got := entityKind(entities, name); got != kind {
			t.Fatalf("%s kind = %q, want %q in %#v", name, got, kind, entities)
		}
	}
	for name, want := range map[string]string{
		"run":   "declare function run",
		"View":  "declare class View",
		"Api":   "declare interface Api",
		"build": "export declare const build",
	} {
		assertEntitySignatureContains(t, entities, name, want)
	}
	if kind := entityKind(entities, "config"); kind != "" {
		t.Fatalf("object-typed const leaked as function entity kind %q in %#v", kind, entities)
	}
}

func TestTreeSitterParserTypeScriptEnumsAndNamespaces(t *testing.T) {
	entities, language := TreeSitterParser{}.Parse("api.ts", `export enum Status { Ready, Failed }

declare namespace Api {
  export function run(value: string): string
  export const build: (value: string) => string
}
`)
	if language != "TypeScript" {
		t.Fatalf("language = %q", language)
	}
	for name, kind := range map[string]string{
		"Status":    "enum",
		"Api.run":   "method",
		"Api.build": "method",
	} {
		if got := entityKind(entities, name); got != kind {
			t.Fatalf("%s kind = %q, want %q in %#v", name, got, kind, entities)
		}
	}
	for _, name := range []string{"run", "build"} {
		if kind := entityKind(entities, name); kind != "" {
			t.Fatalf("namespace member %s leaked as top-level kind %q in %#v", name, kind, entities)
		}
	}
	assertEntitySignatureContains(t, entities, "Status", "export enum Status")
	assertEntitySignatureContains(t, entities, "Api.run", "export function run")
	assertEntitySignatureContains(t, entities, "Api.build", "export const build")
}

func TestTreeSitterParserJavaScriptObjectFunctionMembers(t *testing.T) {
	entities, language := TreeSitterParser{}.Parse("api.js", `const api = {
  run(value) { return value },
  save: (value) => value,
  load: function(value) { return value },
  get name() { return "" },
  nested: {
    parse(value) { return value },
  },
}

module.exports = {
  build(value) { return value },
  create: (value) => value,
}

export default {
  render(value) { return value },
}
`)
	if language != "JavaScript" {
		t.Fatalf("language = %q", language)
	}
	seen := map[string]string{}
	for _, entity := range entities {
		seen[entity.Name] = entity.Kind
	}
	for name, kind := range map[string]string{
		"api.run":               "method",
		"api.save":              "method",
		"api.load":              "method",
		"api.name":              "getter",
		"api.nested.parse":      "method",
		"module.exports.build":  "method",
		"module.exports.create": "method",
		"default.render":        "method",
	} {
		if seen[name] != kind {
			t.Fatalf("%s kind = %q, want %q in %#v", name, seen[name], kind, entities)
		}
	}
}

func TestTreeSitterParserIgnoresAnonymousObjectFunctionMembers(t *testing.T) {
	entities, language := TreeSitterParser{}.Parse("callback.js", `configure({
  run(value) { return value },
  save: (value) => value,
  get name() { return "" },
})
`)
	if language != "JavaScript" {
		t.Fatalf("language = %q", language)
	}
	for _, entity := range entities {
		switch entity.Name {
		case "run", "save", "name":
			t.Fatalf("anonymous object member leaked as entity: %#v in %#v", entity, entities)
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
interface Api { request?: (path: string) => Promise<string> }
type Bar = string
type Contract = { parse(input: string): number; load: (id: string) => string; label: string }
abstract class Base { abstract run(value: string): boolean }
class User {
  validate(value: string) { return value }
  save = (value: string) => value
}
const build = (value: number) => value + 1
`,
			names: []string{"Foo", "Foo.validate", "Api", "Api.request", "Bar", "Contract", "Contract.parse", "Contract.load", "Base", "Base.run", "User", "User.validate", "User.save", "build"},
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
impl std::fmt::Display for User { fn fmt(&self, f: &mut Formatter<'_>) -> Result { Ok(()) } }
impl<T: Clone> IntoIterator for Bag<T> { fn into_iter(self) -> Iter<T> { todo!() } }
`,
			names: []string{"User", "Bag", "validate", "Run", "Run.run", "User.active", "Bag.unwrap_owned", "User.fmt", "Bag.into_iter"},
		},
		{
			path:     ".github/workflows/ci.yml",
			language: "YAML",
			input: `name: CI
on:
  push:
    branches: [main]
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: go test ./...
  deploy:
    if: github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    steps:
      - run: ./scripts/deploy.sh
`,
			names: []string{"ci", "on", "permissions", "jobs.test", "jobs.deploy"},
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

func TestTreeSitterParserSupportsYAMLWorkflowExtensions(t *testing.T) {
	if !Supported(".github/workflows/ci.yml") {
		t.Fatal(".yml workflow should be supported")
	}
	if !Supported(".github/workflows/deploy.yaml") {
		t.Fatal(".yaml workflow should be supported")
	}

	entities, language := TreeSitterParser{}.Parse(".github/workflows/deploy.yaml", `name: Deploy
on: workflow_dispatch
jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - run: echo deploy
`)
	if language != "YAML" {
		t.Fatalf("language = %q", language)
	}
	seen := map[string]string{}
	for _, entity := range entities {
		seen[entity.Name] = entity.Kind
	}
	for name, kind := range map[string]string{
		"deploy":       "workflow",
		"on":           "section",
		"jobs.publish": "job",
	} {
		if seen[name] != kind {
			t.Fatalf("%s kind = %q, want %q in %#v", name, seen[name], kind, entities)
		}
	}
}

func TestTreeSitterParserIgnoresNonFunctionTypeScriptProperties(t *testing.T) {
	entities, language := TreeSitterParser{}.Parse("app.ts", `interface Api {
  url: string
  request: (path: string) => Promise<string>
}

type Config = {
  retries: number
  onError?: (error: Error) => void
}
`)
	if language != "TypeScript" {
		t.Fatalf("language = %q", language)
	}
	seen := map[string]string{}
	for _, entity := range entities {
		seen[entity.Name] = entity.Kind
	}
	for name, kind := range map[string]string{
		"Api.request":    "method",
		"Config.onError": "method",
	} {
		if seen[name] != kind {
			t.Fatalf("%s kind = %q, want %q in %#v", name, seen[name], kind, entities)
		}
	}
	for _, name := range []string{"Api.url", "Config.retries"} {
		if _, ok := seen[name]; ok {
			t.Fatalf("non-function property %s leaked as entity in %#v", name, entities)
		}
	}
}
