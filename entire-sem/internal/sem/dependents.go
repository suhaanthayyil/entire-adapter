package sem

import (
	"context"
	"regexp"
	"strings"

	"github.com/suhaanthayyil/entire-sem/internal/gitutil"
)

var identifierBoundary = regexp.MustCompile(`[A-Za-z0-9_$]+`)

type referenceIndex map[string]map[string]struct{}

type referenceTarget struct {
	Name           string
	ShortName      string
	AmbiguousShort bool
}

type parsedReferenceFile struct {
	Path     string
	Lines    []string
	Entities []Entity
}

func addDependentCounts(ctx context.Context, repo, head string, result *Result) error {
	targets := changedReferenceTargets(*result)
	if len(targets) == 0 {
		return nil
	}

	index, targets, err := buildReferenceIndex(ctx, repo, head, targets)
	if err != nil {
		return err
	}

	for fileIndex := range result.Files {
		for changeIndex := range result.Files[fileIndex].Changes {
			change := &result.Files[fileIndex].Changes[changeIndex]
			key := referenceKey(*change)
			target, ok := targets[key]
			if !ok {
				continue
			}
			change.DependentsCount = len(index[key])
			change.DependentsAmbiguous = target.AmbiguousShort
		}
	}
	return nil
}

func changedReferenceTargets(result Result) map[string]referenceTarget {
	names := map[string]string{}
	shortCounts := map[string]int{}
	for _, file := range result.Files {
		for _, change := range file.Changes {
			name := referenceEntityName(change)
			if name != "" {
				if _, exists := names[name]; !exists {
					shortCounts[shortEntityName(name)]++
				}
				names[name] = name
			}
		}
	}

	targets := map[string]referenceTarget{}
	for key, name := range names {
		shortName := shortEntityName(name)
		targets[key] = referenceTarget{
			Name:           name,
			ShortName:      shortName,
			AmbiguousShort: shortCounts[shortName] > 1,
		}
	}
	return targets
}

func buildReferenceIndex(ctx context.Context, repo, head string, targets map[string]referenceTarget) (referenceIndex, map[string]referenceTarget, error) {
	files, err := gitutil.ListFiles(ctx, repo, head)
	if err != nil {
		return nil, nil, err
	}

	parser := TreeSitterParser{}
	index := referenceIndex{}
	for key := range targets {
		index[key] = map[string]struct{}{}
	}

	var parsedFiles []parsedReferenceFile
	seenEntityNames := map[string]struct{}{}
	shortCounts := map[string]int{}
	for _, path := range files {
		if !Supported(path) {
			continue
		}
		content, ok, err := gitutil.ShowFile(ctx, repo, head, path)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			continue
		}

		entities, _ := parser.Parse(path, content)
		for _, entity := range entities {
			if _, exists := seenEntityNames[entity.Name]; exists {
				continue
			}
			seenEntityNames[entity.Name] = struct{}{}
			shortCounts[shortEntityName(entity.Name)]++
		}
		parsedFiles = append(parsedFiles, parsedReferenceFile{
			Path:     path,
			Lines:    strings.Split(content, "\n"),
			Entities: entities,
		})
	}

	targets = resolveReferenceAmbiguity(targets, shortCounts, seenEntityNames)
	for _, parsed := range parsedFiles {
		for _, entity := range parsed.Entities {
			block := entityBlock(parsed.Lines, entity)
			for key, target := range targets {
				if isSameEntityReference(entity, target) {
					continue
				}
				if target.AmbiguousShort && target.Name != target.ShortName {
					if containsQualifiedReference(block, target.Name) {
						index[key][parsed.Path+"#"+entity.Kind+":"+entity.Name] = struct{}{}
					}
					continue
				}
				if containsIdentifier(block, target.ShortName) {
					index[key][parsed.Path+"#"+entity.Kind+":"+entity.Name] = struct{}{}
				}
			}
		}
	}

	return index, targets, nil
}

func resolveReferenceAmbiguity(targets map[string]referenceTarget, shortCounts map[string]int, entityNames map[string]struct{}) map[string]referenceTarget {
	resolved := make(map[string]referenceTarget, len(targets))
	for key, target := range targets {
		count := shortCounts[target.ShortName]
		if _, exists := entityNames[target.Name]; !exists {
			count++
		}
		if count > 1 {
			target.AmbiguousShort = true
		}
		resolved[key] = target
	}
	return resolved
}

func entityBlock(lines []string, entity Entity) string {
	start := entity.StartLine - 1
	if start < 0 {
		start = 0
	}
	end := entity.EndLine
	if end > len(lines) {
		end = len(lines)
	}
	if end <= start {
		return ""
	}
	return strings.Join(lines[start:end], "\n")
}

func containsIdentifier(content, name string) bool {
	for _, token := range identifierBoundary.FindAllString(content, -1) {
		if token == name {
			return true
		}
	}
	return false
}

func containsQualifiedReference(content, name string) bool {
	candidates := []string{name}
	if strings.Contains(name, ".") {
		candidates = append(candidates, strings.ReplaceAll(name, ".", "::"))
	}
	for _, candidate := range candidates {
		if containsSymbolReference(content, candidate) {
			return true
		}
	}
	return false
}

func containsSymbolReference(content, symbol string) bool {
	pattern := regexp.MustCompile(`(^|[^A-Za-z0-9_$])` + regexp.QuoteMeta(symbol) + `([^A-Za-z0-9_$]|$)`)
	return pattern.FindStringIndex(content) != nil
}

func isSameEntityReference(entity Entity, target referenceTarget) bool {
	if entity.Name == target.Name {
		return true
	}
	return scopesChildren(entity.Kind) && strings.HasPrefix(target.Name, entity.Name+".")
}

func referenceKey(change EntityChange) string {
	return referenceEntityName(change)
}

func referenceEntityName(change EntityChange) string {
	switch change.Type {
	case "renamed":
		if change.NewName != "" {
			return change.NewName
		}
		if change.OldName != "" {
			return change.OldName
		}
	}
	return change.Name
}

func shortEntityName(name string) string {
	if index := strings.LastIndex(name, "."); index >= 0 {
		return name[index+1:]
	}
	return name
}
