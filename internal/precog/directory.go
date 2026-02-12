package precog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

type packageBucket struct {
	name       string
	path       string
	configFile string
	files      []string
}

// AnalyzeDirectoryStructure maps tracked files into directory packages/modules.
func AnalyzeDirectoryStructure(repoPath string, files []string) ([]Package, map[string]string) {
	configByDir := detectConfigFiles(repoPath, files)

	packagesByPath := map[string]*packageBucket{}
	fileToPackage := map[string]string{}

	repoName := filepath.Base(repoPath)

	for _, file := range files {
		pkgPath := filepath.ToSlash(filepath.Dir(file))
		if pkgPath == "" {
			pkgPath = "."
		}
		configFile := nearestConfigFile(pkgPath, configByDir)

		b, ok := packagesByPath[pkgPath]
		if !ok {
			name := filepath.Base(pkgPath)
			if pkgPath == "." {
				name = repoName
			}
			b = &packageBucket{
				name:       name,
				path:       pkgPath,
				configFile: configFile,
				files:      []string{},
			}
			packagesByPath[pkgPath] = b
		}
		b.files = append(b.files, file)
		fileToPackage[file] = pkgPath
	}

	packages := make([]Package, 0, len(packagesByPath))
	for _, b := range packagesByPath {
		sort.Strings(b.files)
		packages = append(packages, Package{
			Name:       b.name,
			Path:       b.path,
			Files:      b.files,
			ConfigFile: b.configFile,
		})
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].Path < packages[j].Path
	})

	return packages, fileToPackage
}

func detectConfigFiles(repoPath string, files []string) map[string]string {
	result := map[string]string{}
	for _, file := range files {
		base := filepath.Base(file)
		if base != "go.mod" && base != "package.json" {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(file))
		if dir == "" {
			dir = "."
		}
		result[dir] = filepath.ToSlash(file)
	}

	// If package.json exists, use its "name" only as a validation read so
	// malformed JSON is ignored rather than polluting package metadata.
	for dir, cfg := range result {
		if filepath.Base(cfg) != "package.json" {
			continue
		}
		cfgPath := filepath.Join(repoPath, filepath.FromSlash(cfg))
		if !isValidPackageJSON(cfgPath) {
			delete(result, dir)
		}
	}

	return result
}

func nearestConfigFile(pkgPath string, configByDir map[string]string) string {
	current := pkgPath
	for {
		if cfg, ok := configByDir[current]; ok {
			return cfg
		}
		if current == "." {
			return ""
		}
		next := filepath.ToSlash(filepath.Dir(current))
		if next == "" || next == current {
			current = "."
		} else {
			current = next
		}
	}
}

func isValidPackageJSON(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var payload map[string]any
	return json.Unmarshal(data, &payload) == nil
}
