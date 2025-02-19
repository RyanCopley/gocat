package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	magicHeader     = "// --------- gocat v1"
	fileStartFormat = "// --------- FILE START: \"%s\" (size: %d bytes, modtime: %s) ----------\n"
	fileEndFormat   = "// --------- FILE END: \"%s\" ----------\n"
	fileStartPrefix = "// --------- FILE START: "
	fileEndPrefix   = "// --------- FILE END: "
	// GitHub repository info (change these to match your public repo)
	repoOwner = "yourusername"
	repoName  = "gocat"
)

var (
	// version is set via ldflags (default "dev")
	version string = "dev"

	// Global variables for exclusion filters.
	excludePackages []string
	excludeFiles    []string

	// Global variable for base package used for Java/Kotlin dependency resolution.
	javaBase string
)

func main() {
	if len(os.Args) < 2 {
		printGeneralHelp()
		os.Exit(1)
	}

	command := os.Args[1]

	// Do not check for updates if running join (to avoid breaking streaming output)
	if command != "join" {
		checkForUpdates()
	}

	switch command {
	case "join":
		joinCmd := flag.NewFlagSet("join", flag.ExitOnError)
		excludePkgs := joinCmd.String("exclude-packages", "", "Comma-separated package names to exclude (based on the file's package declaration)")
		excludeFilesFlag := joinCmd.String("exclude-files", "", "Comma-separated file patterns to exclude")
		javaBaseFlag := joinCmd.String("java-base", "", "Base package for Java/Kotlin recursive dependency resolution")
		goBaseFlag := joinCmd.String("go-base", "", "Base module for Go recursive dependency resolution (overrides go.mod)")
		if err := joinCmd.Parse(os.Args[2:]); err != nil {
			log.Fatalf("Error parsing join command: %v", err)
		}
		if joinCmd.NArg() == 0 {
			log.Fatal("Usage: join [file or glob pattern] ...")
		}
		if *excludePkgs != "" {
			for _, pkg := range strings.Split(*excludePkgs, ",") {
				excludePackages = append(excludePackages, strings.TrimSpace(pkg))
			}
		}
		if *excludeFilesFlag != "" {
			for _, file := range strings.Split(*excludeFilesFlag, ",") {
				excludeFiles = append(excludeFiles, strings.TrimSpace(file))
			}
		}
		javaBase = strings.TrimSpace(*javaBaseFlag)
		if javaBase == "" {
			if jb, err := getJavaModuleName(); err == nil {
				javaBase = jb
			} else {
				log.Printf("Warning: unable to auto-detect Java base package: %v", err)
			}
		}

		var moduleName string
		if *goBaseFlag != "" {
			moduleName = strings.TrimSpace(*goBaseFlag)
		} else {
			var err error
			moduleName, err = getModuleName()
			if err != nil {
				log.Fatalf("Error reading go.mod: %v", err)
			}
		}

		var buf bytes.Buffer
		processed := make(map[string]bool)
		for _, pattern := range joinCmd.Args() {
			pattern = filepath.Clean(pattern)
			matches, err := filepath.Glob(pattern)
			if err != nil {
				log.Printf("Invalid glob pattern %q: %v", pattern, err)
				continue
			}
			if len(matches) == 0 {
				log.Printf("No matches found for pattern %q", pattern)
				continue
			}
			for _, file := range matches {
				file = filepath.Clean(file)
				if err := processFile(file, moduleName, processed, &buf); err != nil {
					log.Printf("Error processing %s: %v", file, err)
				}
			}
		}
		if buf.Len() > 0 {
			fmt.Println(magicHeader)
			fmt.Print(buf.String())
		}
	case "split":
		splitCmd := flag.NewFlagSet("split", flag.ExitOnError)
		inputFile := splitCmd.String("in", "", "Input file to split (default: STDIN)")
		outputDir := splitCmd.String("out", "", "Output directory (default: current directory)")
		if err := splitCmd.Parse(os.Args[2:]); err != nil {
			log.Fatalf("Error parsing split command: %v", err)
		}
		var in io.Reader
		if *inputFile != "" {
			*inputFile = filepath.Clean(*inputFile)
			f, err := os.Open(*inputFile)
			if err != nil {
				log.Fatalf("Error opening input file %q: %v", *inputFile, err)
			}
			defer func() {
				if err := f.Close(); err != nil {
					log.Printf("Error closing input file: %v", err)
				}
			}()
			in = f
		} else {
			in = os.Stdin
		}
		if err := splitInput(in, *outputDir); err != nil {
			log.Fatalf("Error splitting input: %v", err)
		}
	case "help":
		if len(os.Args) == 2 {
			printGeneralHelp()
		} else {
			printSubcommandHelp(os.Args[2])
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printGeneralHelp()
		os.Exit(1)
	}
}

// checkForUpdates queries the GitHub API for the latest release and prints a banner
// if the current version is outdated.
func checkForUpdates() {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName)
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Update check failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Update check returned status %d", resp.StatusCode)
		return
	}

	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		log.Printf("Failed to decode update info: %v", err)
		return
	}

	if isOutdated(version, rel.TagName) {
		fmt.Fprintf(os.Stderr, "Update available: version %s is available (you are using %s). Please update!\n", rel.TagName, version)
	}
}

// isOutdated compares semantic versions (assumes a 'v' prefix) and returns true if current is older than latest.
func isOutdated(current, latest string) bool {
	trim := func(s string) string {
		return strings.TrimPrefix(s, "v")
	}
	curParts := strings.Split(trim(current), ".")
	latParts := strings.Split(trim(latest), ".")
	n := len(curParts)
	if len(latParts) < n {
		n = len(latParts)
	}
	for i := 0; i < n; i++ {
		c, err1 := strconv.Atoi(curParts[i])
		l, err2 := strconv.Atoi(latParts[i])
		if err1 != nil || err2 != nil {
			// Fallback to string comparison if parsing fails.
			return current < latest
		}
		if c < l {
			return true
		} else if c > l {
			return false
		}
	}
	return len(latParts) > len(curParts)
}

// getModuleName reads the Go module name from go.mod in the current directory.
func getModuleName() (string, error) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1], nil
			}
		}
	}
	return "", fmt.Errorf("module name not found in go.mod")
}

// getJavaModuleName attempts to extract the base package (group) from common Java build files.
func getJavaModuleName() (string, error) {
	if _, err := os.Stat("pom.xml"); err == nil {
		data, err := os.ReadFile("pom.xml")
		if err != nil {
			return "", err
		}
		re := regexp.MustCompile(`<groupId>\s*([^<\s]+)\s*</groupId>`)
		matches := re.FindSubmatch(data)
		if len(matches) >= 2 {
			return string(matches[1]), nil
		}
		return "", fmt.Errorf("groupId not found in pom.xml")
	}
	if _, err := os.Stat("build.gradle"); err == nil {
		data, err := os.ReadFile("build.gradle")
		if err != nil {
			return "", err
		}
		re := regexp.MustCompile(`(?m)^\s*group\s*=\s*['"]([^'"]+)['"]`)
		matches := re.FindSubmatch(data)
		if len(matches) >= 2 {
			return string(matches[1]), nil
		}
		return "", fmt.Errorf("group not found in build.gradle")
	}
	if _, err := os.Stat("build.gradle.kts"); err == nil {
		data, err := os.ReadFile("build.gradle.kts")
		if err != nil {
			return "", err
		}
		re := regexp.MustCompile(`(?m)^\s*group\s*=\s*["']([^"']+)["']`)
		matches := re.FindSubmatch(data)
		if len(matches) >= 2 {
			return string(matches[1]), nil
		}
		return "", fmt.Errorf("group not found in build.gradle.kts")
	}
	return "", fmt.Errorf("no recognized Java build file found")
}

// isExcludedFile checks if the file (by its relative path) matches any exclusion pattern.
func isExcludedFile(relPath string) bool {
	for _, pattern := range excludeFiles {
		if match, err := filepath.Match(pattern, relPath); err == nil && match {
			return true
		}
	}
	return false
}

// processFile processes any file. For Go, Java, or Kotlin files, it handles them (possibly recursively).
// The output is written to w.
func processFile(filePath, moduleName string, processed map[string]bool, w io.Writer) error {
	filePath = filepath.Clean(filePath)
	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			log.Printf("File not found: %s", filePath)
			return nil
		}
		return err
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}
	relPath, err := filepath.Rel(".", absPath)
	if err != nil {
		relPath = filePath
	}
	if isExcludedFile(relPath) {
		return nil
	}
	if processed[absPath] {
		return nil
	}
	processed[absPath] = true

	switch filepath.Ext(filePath) {
	case ".go":
		if len(excludePackages) > 0 {
			pkg, err := getPackageName(filePath)
			if err != nil {
				log.Printf("Warning: unable to determine package for %s: %v", filePath, err)
			} else {
				for _, ex := range excludePackages {
					if pkg == ex {
						return nil
					}
				}
			}
		}
		return processGoFile(filePath, moduleName, processed, w)
	case ".java":
		return processJavaFile(filePath, javaBase, processed, w)
	case ".kt", ".kts":
		return processKotlinFile(filePath, javaBase, processed, w)
	default:
		return processNonGoFile(filePath, w)
	}
}

// getPackageName parses the Go file to extract its package declaration.
func getPackageName(filePath string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, parser.PackageClauseOnly)
	if err != nil {
		return "", err
	}
	return f.Name.Name, nil
}

// processGoFile processes a Go source file.
func processGoFile(filePath, moduleName string, processed map[string]bool, w io.Writer) error {
	filePath = filepath.Clean(filePath)
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}
	relPath, err := filepath.Rel(".", absPath)
	if err != nil {
		relPath = filePath
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	modTime := info.ModTime().Format(time.RFC3339)
	fmt.Fprintf(w, fileStartFormat, relPath, info.Size(), modTime)
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, f); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		log.Printf("Error closing file %s: %v", filePath, err)
	}
	fmt.Fprintf(w, fileEndFormat, relPath)
	f2, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f2.Close(); cerr != nil {
			log.Printf("Error closing file %s: %v", filePath, cerr)
		}
	}()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, filePath, f2, parser.ImportsOnly)
	if err != nil {
		return err
	}
	for _, imp := range parsed.Imports {
		importPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(importPath, moduleName) {
			continue
		}
		var relDir string
		if importPath == moduleName {
			relDir = "."
		} else if strings.HasPrefix(importPath, moduleName+"/") {
			relDir = strings.TrimPrefix(importPath, moduleName+"/")
		} else {
			continue
		}
		packageDir := filepath.Join(".", relDir)
		packageDir = filepath.Clean(packageDir)
		entries, err := os.ReadDir(packageDir)
		if err != nil {
			log.Printf("Error reading directory %q: %v", packageDir, err)
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			fileInPkg := filepath.Join(packageDir, entry.Name())
			fileInPkg = filepath.Clean(fileInPkg)
			if err := processFile(fileInPkg, moduleName, processed, w); err != nil {
				log.Printf("Error processing %s: %v", fileInPkg, err)
			}
		}
	}
	return nil
}

// processJavaFile processes a Java source file.
func processJavaFile(filePath, base string, processed map[string]bool, w io.Writer) error {
	filePath = filepath.Clean(filePath)
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}
	relPath, err := filepath.Rel(".", absPath)
	if err != nil {
		relPath = filePath
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	modTime := info.ModTime().Format(time.RFC3339)
	fmt.Fprintf(w, fileStartFormat, relPath, info.Size(), modTime)
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, f); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		log.Printf("Error closing file %s: %v", filePath, err)
	}
	fmt.Fprintf(w, fileEndFormat, relPath)
	f2, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f2.Close()
	scanner := bufio.NewScanner(f2)
	importRegex := regexp.MustCompile(`^\s*import\s+([a-zA-Z0-9_.]+);`)
	for scanner.Scan() {
		line := scanner.Text()
		matches := importRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		importPath := matches[1]
		if importPath == base || strings.HasPrefix(importPath, base+".") {
			var relDir string
			if importPath == base {
				relDir = "."
			} else {
				relDir = strings.TrimPrefix(importPath, base+".")
				relDir = filepath.FromSlash(strings.ReplaceAll(relDir, ".", "/"))
			}
			packageDir := filepath.Join(".", relDir)
			packageDir = filepath.Clean(packageDir)
			entries, err := os.ReadDir(packageDir)
			if err != nil {
				log.Printf("Error reading directory %q: %v", packageDir, err)
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				if filepath.Ext(entry.Name()) != ".java" {
					continue
				}
				fileInPkg := filepath.Join(packageDir, entry.Name())
				fileInPkg = filepath.Clean(fileInPkg)
				if err := processFile(fileInPkg, base, processed, w); err != nil {
					log.Printf("Error processing %s: %v", fileInPkg, err)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

// processKotlinFile processes a Kotlin source file (.kt or .kts).
func processKotlinFile(filePath, base string, processed map[string]bool, w io.Writer) error {
	filePath = filepath.Clean(filePath)
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}
	relPath, err := filepath.Rel(".", absPath)
	if err != nil {
		relPath = filePath
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	modTime := info.ModTime().Format(time.RFC3339)
	fmt.Fprintf(w, fileStartFormat, relPath, info.Size(), modTime)
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, f); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		log.Printf("Error closing file %s: %v", filePath, err)
	}
	fmt.Fprintf(w, fileEndFormat, relPath)
	f2, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f2.Close()
	scanner := bufio.NewScanner(f2)
	importRegex := regexp.MustCompile(`^\s*import\s+([a-zA-Z0-9_.]+);?`)
	for scanner.Scan() {
		line := scanner.Text()
		matches := importRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		importPath := matches[1]
		if importPath == base || strings.HasPrefix(importPath, base+".") {
			var relDir string
			if importPath == base {
				relDir = "."
			} else {
				relDir = strings.TrimPrefix(importPath, base+".")
				relDir = filepath.FromSlash(strings.ReplaceAll(relDir, ".", "/"))
			}
			packageDir := filepath.Join(".", relDir)
			packageDir = filepath.Clean(packageDir)
			entries, err := os.ReadDir(packageDir)
			if err != nil {
				log.Printf("Error reading directory %q: %v", packageDir, err)
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				ext := filepath.Ext(entry.Name())
				if ext != ".kt" && ext != ".kts" {
					continue
				}
				fileInPkg := filepath.Join(packageDir, entry.Name())
				fileInPkg = filepath.Clean(fileInPkg)
				if err := processFile(fileInPkg, base, processed, w); err != nil {
					log.Printf("Error processing %s: %v", fileInPkg, err)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

// processNonGoFile outputs a non-source file with header and footer delimiters.
func processNonGoFile(filePath string, w io.Writer) error {
	filePath = filepath.Clean(filePath)
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}
	relPath, err := filepath.Rel(".", absPath)
	if err != nil {
		relPath = filePath
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	modTime := info.ModTime().Format(time.RFC3339)
	fmt.Fprintf(w, fileStartFormat, relPath, info.Size(), modTime)
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, f); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		log.Printf("Error closing file %s: %v", filePath, err)
	}
	fmt.Fprintf(w, fileEndFormat, relPath)
	return nil
}

// splitInput reads a joined stream from r and recreates each file based on the delimiters.
func splitInput(r io.Reader, outDir string) error {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return fmt.Errorf("input is empty, missing magic header")
	}
	firstLine := scanner.Text()
	if !strings.HasPrefix(firstLine, magicHeader) {
		return fmt.Errorf("invalid magic header: %s", firstLine)
	}
	var absOutDir string
	var err error
	if outDir != "" {
		absOutDir, err = filepath.Abs(filepath.Clean(outDir))
		if err != nil {
			return fmt.Errorf("failed to get absolute path for output directory: %v", err)
		}
	}
	var currentFile *os.File
	var currentFilename string
	inFile := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, fileStartPrefix) {
			startQuote := strings.Index(line, "\"")
			if startQuote == -1 {
				log.Printf("Invalid header format: %s", line)
				continue
			}
			endQuote := strings.Index(line[startQuote+1:], "\"")
			if endQuote == -1 {
				log.Printf("Invalid header format: %s", line)
				continue
			}
			filename := line[startQuote+1 : startQuote+1+endQuote]
			filename = filepath.Clean(filename)
			if absOutDir != "" {
				filename = filepath.Join(absOutDir, filename)
				filename = filepath.Clean(filename)
				relToOut, err := filepath.Rel(absOutDir, filename)
				if err != nil || strings.HasPrefix(relToOut, "..") {
					log.Printf("Invalid output file path %q; skipping", filename)
					continue
				}
			}
			if err := os.MkdirAll(filepath.Dir(filename), 0750); err != nil {
				log.Printf("Error creating directories for %q: %v", filename, err)
				continue
			}
			f, err := os.Create(filename)
			if err != nil {
				log.Printf("Error creating file %q: %v", filename, err)
				continue
			}
			currentFile = f
			currentFilename = filename
			inFile = true
			continue
		}
		if strings.HasPrefix(line, fileEndPrefix) && inFile {
			if currentFile != nil {
				if err := currentFile.Close(); err != nil {
					log.Printf("Error closing file %q: %v", currentFilename, err)
				}
			}
			currentFile = nil
			currentFilename = ""
			inFile = false
			continue
		}
		if inFile && currentFile != nil {
			if _, err := currentFile.WriteString(line + "\n"); err != nil {
				log.Printf("Error writing to file %q: %v", currentFilename, err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

// printGeneralHelp prints the general usage message with the version.
func printGeneralHelp() {
	fmt.Printf(`gocat %s
Usage: %s <command> [options] [arguments]

Commands:
  join    Join source files (and their internal dependencies) into a single stream.
  split   Split a joined file into separate files.
  help    Show help information.

For detailed help on a command, run:
  %s help <command>

`, version, "gocat", "gocat")
}

// printSubcommandHelp prints help for a specific subcommand.
func printSubcommandHelp(cmd string) {
	switch cmd {
	case "join":
		fmt.Printf(`Usage: %s join [file or glob pattern] ...

Joins the specified source file(s) into a single output stream,
inserting delimiters between files. For Go files, the tool parses import statements
and recursively includes any files from packages within the same module (from go.mod or -go-base).
For Java/Kotlin files, if a base package is provided via -java-base (or auto-detected), recursive inclusion is performed
by scanning for import statements.
Non-source files are simply included as-is.
Each file is included only once.

Example:
  %s join "main.go" "./pkg/*.go" -exclude-packages="expressions,lexer" -exclude-files="vendor/*,testdata/*" -java-base="com.example" -go-base="github.com/example/project"
`, "gocat", "gocat")
	case "split":
		fmt.Printf(`Usage: %s split [-in inputfile] [-out outputdirectory]

Splits a joined file (or STDIN) into separate files using the inserted delimiters.

Options:
  -in   Input file to split (if omitted, STDIN is used)
  -out  Output directory for the extracted files (default: current directory)

Examples:
  %s split -in joined.txt -out outputFolder
  %s split -out outputFolder < joined.txt>
`, "gocat", "gocat", "gocat")
	default:
		fmt.Printf("Unknown help topic %q. Available topics: join, split\n", cmd)
	}
}
