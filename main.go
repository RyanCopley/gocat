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

const magicHeader = "// --------- gocat v1"

func main() {
	if len(os.Args) < 2 {
		printGeneralHelp()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "join":
		joinCmd := flag.NewFlagSet("join", flag.ExitOnError)
		if err := joinCmd.Parse(os.Args[2:]); err != nil {
			log.Fatalf("Error parsing join command: %v", err)
		}
		if joinCmd.NArg() == 0 {
			log.Fatal("Usage: join [file or glob pattern] ...")
		}

		// Output the magic header first.
		fmt.Println(magicHeader)

		// Read the module name from go.mod.
		moduleName, err := getModuleName()
		if err != nil {
			log.Fatalf("Error reading go.mod: %v", err)
		}

		processed := make(map[string]bool)
		// Process each provided glob pattern.
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
				if err := processFile(file, moduleName, processed); err != nil {
					log.Printf("Error processing %s: %v", file, err)
				}
			}
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
	// Normalize the file path.
	filePath = filepath.Clean(filePath)
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
	return processNonGoFile(filePath)
}

// processGoFile processes a Go source file by printing its header, content, and footer,
// then parsing its import statements to recursively include any internal files.
func processGoFile(filePath, moduleName string, processed map[string]bool) error {
	filePath = filepath.Clean(filePath)
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}
	relPath, err := filepath.Rel(".", absPath)
	if err != nil {
		relPath = filePath
	}

	// Get file info for metadata.
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	modTime := info.ModTime().Format(time.RFC3339)

	// Print header.
	fmt.Printf("// --------- FILE START: \"%s\" (size: %d bytes, modtime: %s) ----------\n", relPath, info.Size(), modTime)

	// Open the file and output its contents.
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(os.Stdout, f); err != nil {
		if cerr := f.Close(); cerr != nil {
			log.Printf("Error closing file %s: %v", filePath, cerr)
		}
		return err
	}
	if err := f.Close(); err != nil {
		log.Printf("Error closing file %s: %v", filePath, err)
	}

	// Print footer.
	fmt.Printf("// --------- FILE END: \"%s\" ----------\n", relPath)

	// Re-open the file for parsing imports.
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

	// Process each import that begins with the module name.
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
			if err := processFile(fileInPkg, moduleName, processed); err != nil {
				log.Printf("Error processing %s: %v", fileInPkg, err)
			}
		}
	}
	return nil
}

// processNonGoFile prints a non-Go file with the appropriate header, content, and footer.
func processNonGoFile(filePath string) error {
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

	// Print header.
	fmt.Printf("// --------- FILE START: \"%s\" (size: %d bytes, modtime: %s) ----------\n", relPath, info.Size(), modTime)

	// Open and copy file content.
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(os.Stdout, f); err != nil {
		if cerr := f.Close(); cerr != nil {
			log.Printf("Error closing file %s: %v", filePath, cerr)
		}
		return err
	}
	if err := f.Close(); err != nil {
		log.Printf("Error closing file %s: %v", filePath, err)
	}

	// Print footer.
	fmt.Printf("// --------- FILE END: \"%s\" ----------\n", relPath)
	return nil
}

// splitInput reads a joined stream from r and recreates each file based on the delimiters.
func splitInput(r io.Reader, outDir string) error {
	scanner := bufio.NewScanner(r)

	// Validate magic header.
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
	headerPrefix := "// --------- FILE START: "
	footerPrefix := "// --------- FILE END: "

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, headerPrefix) {
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

		if strings.HasPrefix(line, footerPrefix) && inFile {
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
