package gtfs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestReadmeListsSupportedFiles checks that the README's table of GTFS
// Schedule files marks every file the parser reads, as listed in
// constants/constants.go, as supported, so the table cannot fall behind the
// code again.
func TestReadmeListsSupportedFiles(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	// A table row: | [name.txt](link) | status | ...
	rowRe := regexp.MustCompile(`(?m)^\| \[([a-z_]+\.(?:txt|geojson))\]\([^)]*\) *\| *(\S+)`)
	status := map[string]string{}
	for _, m := range rowRe.FindAllStringSubmatch(string(readme), -1) {
		status[m[1]] = m[2]
	}
	if len(status) == 0 {
		t.Fatal("found no file rows in README.md")
	}
	read := map[string]bool{}
	for _, file := range staticFileConstants(t) {
		read[file] = true
		switch got, ok := status[file]; {
		case !ok:
			t.Errorf("README.md has no row for %s, which the parser reads", file)
		case got != "✅":
			t.Errorf("README.md marks %s as %s, but the parser reads it", file, got)
		}
	}
	for file, got := range status {
		if got == "✅" && !read[file] {
			t.Errorf("README.md marks %s as supported, but constants/constants.go does not declare it", file)
		}
	}
}

// staticFileConstants returns the file names constants/constants.go
// declares.
func staticFileConstants(t *testing.T) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "constants/constants.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	ast.Inspect(f, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil && (strings.HasSuffix(s, ".txt") || strings.HasSuffix(s, ".geojson")) {
				files = append(files, s)
			}
		}
		return true
	})
	return files
}
