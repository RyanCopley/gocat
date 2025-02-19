// gocat.go
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		printGeneralHelp()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "join":
		joinCmd := flag.NewFlagSet("join", flag.ExitOnError)
		joinCmd.Parse(os.Args[2:])
		if joinCmd.NArg() == 0 {
			log.Fatal("Usage: join [file or glob pattern] ...")
		}

		// Read the module name from go.mod.
		moduleName, err := getModuleName()
		if err != nil {
			log.Fatalf("Error reading go.mod: %v", err)
		}

		processed := make(map[string]bool)
		// Process each provided glob pattern.
		for _, pattern := range joinCmd.Args() {
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
				if err := processFile(file, moduleName, processed); err != nil {
					log.Printf("Error processing %s: %v", file, err)
				}
			}
		}
	case "split":
		splitCmd := flag.NewFlagSet("split", flag.ExitOnError)
		inputFile := splitCmd.String("in", "", "Input file to split (default: STDIN)")
		outputDir := splitCmd.String("out", "", "Output directory (default: current directory)")
		splitCmd.Parse(os.Args[2:])

		var in io.Reader
		if *inputFile != "" {
			f, err := os.Open(*inputFile)
			if err != nil {
				log.Fatalf("Error opening input file %q: %v", *inputFile, err)
			}
			defer f.Close()
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

// getModuleName reads the module name from go.mod in the current directory.
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

// processFile is the unified function for processing any file.
// If the file is a Go file, it processes it (and its imports) recursively.
// Otherwise, it simply outputs the file.
func processFile(filePath, moduleName string, processed map[string]bool) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}
	// Avoid processing the same file more than once.
	if processed[absPath] {
		return nil
	}
	processed[absPath] = true

	if filepath.Ext(filePath) == ".go" {
		return processGoFile(filePath, moduleName, processed)
	}
	return processNonGoFile(filePath, processed)
}

// processGoFile processes a Go source file by outputting its contents
// and then parsing its import statements to recursively include any internal files.
func processGoFile(filePath, moduleName string, processed map[string]bool) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}
	// Compute a path relative to the module root (assumed to be current directory).
	relPath, err := filepath.Rel(".", absPath)
	if err != nil {
		relPath = filePath
	}

	// Open the file and output its contents with delimiters.
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	modTime := info.ModTime().Format(time.RFC3339)
	fmt.Printf("// --------- FILE START: \"%s\" (size: %d bytes, modtime: %s) ----------\n",
		relPath, info.Size(), modTime)
	if _, err := io.Copy(os.Stdout, f); err != nil {
		f.Close()
		return err
	}
	f.Close()
	fmt.Print("\n")
	fmt.Printf("// --------- FILE END: \"%s\" ----------\n", relPath)

	// Re-open the file for parsing imports.
	f2, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f2.Close()

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, filePath, f2, parser.ImportsOnly)
	if err != nil {
		return err
	}

	// Process each import that begins with the module name.
	for _, imp := range parsed.Imports {
		importPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(importPath, moduleName) {
			continue
		}

		// Determine the package directory.
		var relDir string
		if importPath == moduleName {
			relDir = "."
		} else if strings.HasPrefix(importPath, moduleName+"/") {
			relDir = strings.TrimPrefix(importPath, moduleName+"/")
		} else {
			continue
		}
		packageDir := filepath.Join(".", relDir)

		// List **all** files in the package directory and process each one.
		entries, err := os.ReadDir(packageDir)
		if err != nil {
			log.Printf("Error reading directory %q: %v", packageDir, err)
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				// Skip subdirectories.
				continue
			}
			fileInPkg := filepath.Join(packageDir, entry.Name())
			if err := processFile(fileInPkg, moduleName, processed); err != nil {
				log.Printf("Error processing %s: %v", fileInPkg, err)
			}
		}
	}
	return nil
}

// processNonGoFile simply outputs a non-Go file with the appropriate delimiters.
func processNonGoFile(filePath string, processed map[string]bool) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}
	relPath, err := filepath.Rel(".", absPath)
	if err != nil {
		relPath = filePath
	}

	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	modTime := info.ModTime().Format(time.RFC3339)
	fmt.Printf("// --------- FILE START: \"%s\" (size: %d bytes, modtime: %s) ----------\n",
		relPath, info.Size(), modTime)
	if _, err := io.Copy(os.Stdout, f); err != nil {
		f.Close()
		return err
	}
	f.Close()
	fmt.Print("\n")
	fmt.Printf("// --------- FILE END: \"%s\" ----------\n", relPath)
	return nil
}

// splitInput reads a joined stream from r and recreates each file based on the delimiters.
func splitInput(r io.Reader, outDir string) error {
	scanner := bufio.NewScanner(r)
	var currentFile *os.File
	var currentFilename string
	inFile := false
	headerPrefix := "// --------- FILE START: "
	footerPrefix := "// --------- FILE END: "

	for scanner.Scan() {
		line := scanner.Text()

		// Detect header delimiter.
		if strings.HasPrefix(line, headerPrefix) {
			// Expected header format:
			// // --------- FILE START: "filename" (size: ... bytes, modtime: ...) ----------
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
			if outDir != "" {
				filename = filepath.Join(outDir, filename)
			}
			// Create directory structure as needed.
			if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
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

		// Detect footer delimiter.
		if strings.HasPrefix(line, footerPrefix) && inFile {
			currentFile.Close()
			currentFile = nil
			currentFilename = ""
			inFile = false
			continue
		}

		// Write file content lines.
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

// printGeneralHelp prints the general usage message.
func printGeneralHelp() {
	fmt.Printf(`Usage: %s <command> [options]

Commands:
  join    Join Go source files (and their internal module dependencies) into a single stream.
          Non-Go files are included as-is.
  split   Split a joined file into separate source (or non-source) files.
  help    Show help information.

For detailed help on a command, run:
  %s help <command>

`, "gocat", "gocat")
}

// printSubcommandHelp prints help for a specific subcommand.
func printSubcommandHelp(cmd string) {
	switch cmd {
	case "join":
		fmt.Printf(`Usage: %s join [file or glob pattern] ...

Joins the specified Go source file(s) into a single output stream,
inserting delimiters between files. For each Go file, the tool parses its imports
and recursively includes any files from packages within the same module (as determined by go.mod).
Non-Go files are simply included as-is.
Each file is included only once.

Example:
  %s join "main.go" "./pkg/*.go"

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
