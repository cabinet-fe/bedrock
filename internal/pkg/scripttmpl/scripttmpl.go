// Package scripttmpl expands ${{...}} placeholders in CI/CD scripts.
//
// Syntax (one-shot text replace, not shell evaluation):
//   ${{ workspace }}
//   ${{ job.id }} / ${{ run.build_number }} / …
//   ${{ env.KEY }}
//
// Identifiers: [A-Za-z_][A-Za-z0-9_]* ; dotted paths allowed.
// Unknown variables fail; values are not re-expanded.
package scripttmpl

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// placeholderRE matches ${{ path }} with optional whitespace around the path.
var placeholderRE = regexp.MustCompile(`\$\{\{\s*([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\s*\}\}`)

// Expand replaces all ${{...}} placeholders in script using vars.
// Keys are full paths (e.g. "job.id", "env.FOO", "workspace").
// Unknown variables return an error; replacement is single-pass (no re-expand).
func Expand(script string, vars map[string]string) (string, error) {
	if vars == nil {
		vars = map[string]string{}
	}
	matches := placeholderRE.FindAllStringSubmatch(script, -1)
	if len(matches) == 0 {
		return script, nil
	}
	unknown := make([]string, 0)
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		name := m[1]
		if _, ok := vars[name]; ok {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		unknown = append(unknown, name)
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return "", fmt.Errorf("未知模板变量: %s", strings.Join(unknown, ", "))
	}
	return placeholderRE.ReplaceAllStringFunc(script, func(match string) string {
		sub := placeholderRE.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		return vars[sub[1]]
	}), nil
}
