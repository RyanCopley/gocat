// main.go
//
// Gocat v0.1 - Ryan Copley (2025-02-18)
//
// This tool bundles multiple files (with recursive resolution for Go files)
// into a single output stream and can later split that stream back into the
// original file hierarchy.

package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Constants for the magic header and file delimiters.
const (
	magicHeader     = "// --------- gocat v1"
	fileStartFormat = "// --------- FILE START: \"%s\" (size: %d bytes, modtime: %s) ----------"
	fileEndFormat   = "// --------- FILE END: \"%s\" ----------"
)

// Global map to track processed files (using their absolute paths).
var processedFiles map[string]bool

// Global slices for exclusion flags.
var excludeFilePatterns []string
var excludePackageNames []string

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Error: No subcommand provided.")
		printGeneralUsage()
		os.Exit(1)
	}
	cmd := os.Args[1]
	switch cmd {
	case "join":
		runJoin(os.Args[2:])
	case "split":
		runSplit(os.Args[2:])
	case "help":
		runHelp(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n", cmd)
		printGeneralUsage()
		os.Exit(1)
	}
}

// printGeneralUsage prints general usage information.
func printGeneralUsage() {
	usage := `Usage: gocat <command> [options] [arguments]
Commands:
   join   Bundles files into a single stream.
   split  Splits a bundled stream into individual files.
   help   Displays usage information.
`
	fmt.Println(usage)
}

// runJoin implements the "join" subcommand.
func runJoin(args []string) {
	// Define join-specific flags.
	fs := flag.NewFlagSet("join", flag.ExitOnError)
	exclFiles := fs.String("exclude-files", "", "Comma-separated glob patterns for files to exclude")
	exclPkgs := fs.String("exclude-packages", "", "Comma-separated package names (or prefixes) to exclude from recursive processing")
	fs.Parse(args)

	// The remaining arguments are file or glob patterns.
	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "Error: join requires at least one file or glob pattern.")
		os.Exit(1)
	}

	// Process comma-separated exclusion patterns.
	if *exclFiles != "" {
		excludeFilePatterns = splitAndTrim(*exclFiles)
	}
	if *exclPkgs != "" {
		excludePackageNames = splitAndTrim(*exclPkgs)
	}

	// Output the magic header first.
	fmt.Println(magicHeader)

	// Retrieve the module name from go.mod.
	moduleName, err := getModuleName()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error retrieving module name: %v\n", err)
		os.Exit(1)
	}

	// Initialize the processed-files map.
	processedFiles = make(map[string]bool)

	// Process each provided file or glob pattern.
	for _, pattern := range files {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid glob pattern '%s': %v\n", pattern, err)
			continue
		}
		if len(matches) == 0 {
			fmt.Fprintf(os.Stderr, "Warning: pattern '%s' did not match any files.\n", pattern)
		}
		for _, match := range matches {
			processPath(match, moduleName)
		}
	}
}

// splitAndTrim splits a comma-separated string and trims whitespace.
func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return parts
}

// processPath handles a given path. If it is a directory, it walks recursively.
func processPath(path, moduleName string) {
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error accessing path %s: %v\n", path, err)
		return
	}
	if info.IsDir() {
		// Walk the directory recursively.
		filepath.Walk(path, func(subpath string, subinfo os.FileInfo, err error) error {
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error accessing %s: %v\n", subpath, err)
				return nil
			}
			if !subinfo.IsDir() {
				processFile(subpath, moduleName)
			}
			return nil
		})
	} else {
		processFile(path, moduleName)
	}
}

// processFile writes a file's contents with header/footer delimiters
// and, if it is a Go file, processes its import statements recursively.
func processFile(filePath, moduleName string) {
	// Get the absolute path to avoid duplicate processing.
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting absolute path for %s: %v\n", filePath, err)
		return
	}
	if processedFiles[absPath] {
		return
	}

	// Compute a relative path (for display in the delimiters).
	cwd, _ := os.Getwd()
	relPath, err := filepath.Rel(cwd, absPath)
	if err != nil {
		relPath = filePath // fallback to the original path
	}

	// Check if the file should be excluded.
	if isExcludedFile(relPath) {
		return
	}

	processedFiles[absPath] = true

	// Get file information.
	info, err := os.Stat(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error stating file %s: %v\n", filePath, err)
		return
	}

	// Read the file content.
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file %s: %v\n", filePath, err)
		return
	}

	// Output the header delimiter.
	fmt.Printf(fileStartFormat+"\n", relPath, info.Size(), info.ModTime().Format(time.RFC3339))
	// Output the file content.
	fmt.Print(string(content))
	// Output the footer delimiter.
	fmt.Printf(fileEndFormat+"\n", relPath)

	// For Go files, parse and process import statements.
	if strings.HasSuffix(filePath, ".go") {
		processGoFileForImports(filePath, moduleName)
	}
}

// isExcludedFile returns true if the file's relative path matches any exclusion pattern.
func isExcludedFile(relPath string) bool {
	for _, pattern := range excludeFilePatterns {
		match, err := filepath.Match(pattern, relPath)
		if err == nil && match {
			return true
		}
	}
	return false
}

// processGoFileForImports parses a Go file for import statements and,
// for each import that starts with the module name (unless excluded), recursively processes
// the package directory.
func processGoFileForImports(filePath, moduleName string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, parser.ImportsOnly)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing Go file %s: %v\n", filePath, err)
		return
	}
	for _, imp := range f.Imports {
		// Remove quotes from the import path.
		importPath := strings.Trim(imp.Path.Value, "\"")

		// Check if the import path should be excluded.
		skip := false
		for _, excl := range excludePackageNames {
			// Exact match or if the import path has the exclusion as a prefix (followed by a slash).
			if importPath == excl || strings.HasPrefix(importPath, excl+"/") {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		if strings.HasPrefix(importPath, moduleName) {
			// Compute the package directory.
			subPath := strings.TrimPrefix(importPath, moduleName)
			subPath = strings.TrimPrefix(subPath, "/")
			var pkgDir string
			if subPath == "" {
				// Importing the module root.
				pkgDir = "."
			} else {
				pkgDir = subPath
			}
			// Get the absolute path of the package directory.
			absPkgDir, err := filepath.Abs(pkgDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting absolute path for package directory %s: %v\n", pkgDir, err)
				continue
			}
			// Walk the package directory and process all .go files.
			err = filepath.Walk(absPkgDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error accessing %s: %v\n", path, err)
					return nil
				}
				if info.IsDir() {
					return nil
				}
				if strings.HasSuffix(path, ".go") {
					processFile(path, moduleName)
				}
				return nil
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error walking package directory %s: %v\n", absPkgDir, err)
			}
		}
	}
}

// getModuleName reads the go.mod file in the current directory and extracts
// the module name from the line beginning with "module ".
func getModuleName() (string, error) {
	data, err := ioutil.ReadFile("go.mod")
	if err != nil {
		return "", fmt.Errorf("go.mod file not found")
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", fmt.Errorf("module name not found in go.mod")
}

// runSplit implements the "split" subcommand.
func runSplit(args []string) {
	// Define flags.
	fs := flag.NewFlagSet("split", flag.ExitOnError)
	inFile := fs.String("in", "", "Input file (defaults to stdin)")
	outDir := fs.String("out", ".", "Output directory")
	fs.Parse(args)

	// Open the input stream.
	var reader io.Reader
	if *inFile != "" {
		f, err := os.Open(*inFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening input file %s: %v\n", *inFile, err)
			os.Exit(1)
		}
		defer f.Close()
		reader = f
	} else {
		reader = os.Stdin
	}

	scanner := bufio.NewScanner(reader)
	// Validate the magic header.
	if !scanner.Scan() {
		fmt.Fprintln(os.Stderr, "Error: Input is empty, magic header missing.")
		os.Exit(1)
	}
	firstLine := scanner.Text()
	if firstLine != magicHeader {
		fmt.Fprintln(os.Stderr, "Error: Magic header does not match. Aborting split.")
		os.Exit(1)
	}

	// Compile regexes to match file header and footer delimiters.
	headerRegex := regexp.MustCompile(`^// --------- FILE START: "(.+)" \(size: (\d+) bytes, modtime: (.+)\) ----------$`)
	footerRegex := regexp.MustCompile(`^// --------- FILE END: "(.+)" ----------$`)

	var currentFile *os.File
	var currentFileName string
	for scanner.Scan() {
		line := scanner.Text()
		// Check for a header delimiter.
		if matches := headerRegex.FindStringSubmatch(line); matches != nil {
			// If already writing a file, this is a mismatch.
			if currentFile != nil {
				fmt.Fprintf(os.Stderr, "Error: encountered new file header before previous file ended.\n")
				currentFile.Close()
				currentFile = nil
			}
			currentFileName = matches[1]
			// Build the output file path within the designated output directory.
			outPath := filepath.Join(*outDir, currentFileName)
			// Ensure the output file remains within the output directory.
			if !strings.HasPrefix(filepath.Clean(outPath), filepath.Clean(*outDir)) {
				fmt.Fprintf(os.Stderr, "Error: output file %s is outside of output directory.\n", outPath)
				continue
			}
			// Create necessary directories.
			if err := os.MkdirAll(filepath.Dir(outPath), os.ModePerm); err != nil {
				fmt.Fprintf(os.Stderr, "Error creating directories for %s: %v\n", outPath, err)
				continue
			}
			f, err := os.Create(outPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating file %s: %v\n", outPath, err)
				continue
			}
			currentFile = f
		} else if matches := footerRegex.FindStringSubmatch(line); matches != nil {
			// A footer delimiter has been encountered.
			if currentFile == nil {
				fmt.Fprintf(os.Stderr, "Error: encountered footer without a corresponding header.\n")
				continue
			}
			// Verify that the footer file name matches the header.
			if matches[1] != currentFileName {
				fmt.Fprintf(os.Stderr, "Error: footer file name %s does not match header file name %s.\n", matches[1], currentFileName)
				currentFile.Close()
				currentFile = nil
				continue
			}
			currentFile.Close()
			currentFile = nil
		} else {
			// Regular file content lines.
			if currentFile != nil {
				_, err := currentFile.WriteString(line + "\n")
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error writing to file %s: %v\n", currentFileName, err)
				}
			}
		}
	}

	if currentFile != nil {
		currentFile.Close()
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}
}

// runHelp implements the "help" subcommand.
func runHelp(args []string) {
	if len(args) == 0 {
		printGeneralUsage()
		fmt.Println("Use 'gocat help <command>' for more information on a command.")
		return
	}
	subcommand := args[0]
	switch subcommand {
	case "join":
		helpJoin()
	case "split":
		helpSplit()
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand '%s' for help.\n", subcommand)
		printGeneralUsage()
	}
}

// helpJoin prints detailed help for the join command.
func helpJoin() {
	helpText := `Usage: gocat join [options] [file or glob pattern] ...
Bundles files (and recursively includes Go dependencies) into a single stream.

Options:
  -exclude-files     Comma-separated glob patterns for files to exclude.
  -exclude-packages  Comma-separated package names (or prefixes) to exclude from recursive processing.

Behavior:
  - Reads the go.mod file to extract the module name.
  - Outputs a magic header ("// --------- gocat v1") as the first line.
  - For each file or glob pattern match:
      - For Go files: outputs a header delimiter, file content, then a footer delimiter.
      - Parses Go files for import statements and recursively includes internal module files,
        unless the package is excluded.
      - For non-Go files: outputs a header delimiter, file content, then a footer delimiter.
  - Uses a tracking mechanism to avoid duplicate file processing.
`
	fmt.Println(helpText)
}

// helpSplit prints detailed help for the split command.
func helpSplit() {
	helpText := `Usage: gocat split [-in inputfile] [-out outputdirectory]
Splits a bundled stream (produced by gocat join) back into individual files.

Behavior:
  - Reads from the specified input file or standard input (if -in is not provided).
  - Validates that the first line matches the magic header ("// --------- gocat v1").
  - Processes header and footer delimiters to reconstruct the original file hierarchy.
  - Validates output paths to remain within the designated output directory.
`
	fmt.Println(helpText)
}
