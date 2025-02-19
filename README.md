# gocat

**gocat** is a command-line utility written in Go that helps you bundle relevant source files (including Go, Java, and Kotlin, based on their imports) into a single output stream with embedded metadata delimiters. It also provides a way to split that stream back into the original files, preserving their relative paths. When processing Go files, gocat automatically reads your module name from `go.mod` (or uses a value provided via the `-go-base` flag) and recursively includes any Go source files from packages within your module that are imported by the specified files. Similarly, for Java and Kotlin files, gocat scans import statements and recursively inlines files that belong to the same base package (which can be auto-detected from common build files or specified via the `-java-base` flag). Any file that is not recognized as a supported source file is simply included as-is.

## Features

- **Recursive Go File Bundling:**  
  Reads the module name from `go.mod` (or uses the value provided via the `-go-base` flag) and, for each provided Go file (or glob pattern), outputs the file with header and footer delimiters. It then parses the file for import statements and recursively includes all Go files from packages that belong to the same module.

- **Recursive Java/Kotlin File Bundling:**  
  Scans Java (`.java`) and Kotlin (`.kt`/`.kts`) files for import statements. If an import belongs to the specified base package (which can be auto-detected from `pom.xml`, `build.gradle`, or `build.gradle.kts` or provided via the `-java-base` flag), gocat recursively includes all matching source files.

- **Non-Source File Inclusion:**  
  Any file that is not a recognized source file is output with the same delimiters but is not further processed.

- **No Duplicate Inclusions:**  
  Each file is processed only once, even if it is referenced multiple times or if there are cyclic dependencies between source files.

- **Split Functionality:**  
  Easily split the bundled output back into the original individual files, maintaining relative paths.

- **Glob Support:**  
  Use glob patterns to specify groups of files and directories.

- **Consistent Delimiter Format:**  
  Each file is wrapped in clear header and footer delimiters for easy identification:
  
  ```
  // --------- FILE START: "relative/path/to/file" (size: X bytes, modtime: YYYY-MM-DDTHH:MM:SSZ) ----------
  <file contents>
  // --------- FILE END: "relative/path/to/file" ----------
  ```

- **Built-In Help:**  
  Includes a `help` subcommand to display usage information.

- **Exclusion Options:**  
  - **Exclude Packages:** Use the `-exclude-packages` flag with a comma-separated list of package names to exclude Go files whose package declaration matches one of the specified names.
  - **Exclude Files:** Use the `-exclude-files` flag with a comma-separated list of glob patterns to skip specific files from processing.

## Prerequisites

- Go 1.24 or later

## Installation

Clone the repository and build **gocat**:

```bash
git clone https://github.com/yourusername/gocat.git
cd gocat
go build -o gocat gocat.go
```

This creates an executable named `gocat`.

## Usage

**gocat** supports three subcommands: `join`, `split`, and `help`.

### Join Command

The `join` command reads one or more files or glob patterns and outputs the contents of each file wrapped in header and footer delimiters. For Go files, it parses import statements to recursively include Go files from your module. For Java and Kotlin files, it scans for import statements and recursively includes source files from the same base package.

#### Syntax

```bash
./gocat join [file or glob pattern] ... [options]
```

#### Examples

- **Join a Single Go File:**

  ```bash
  ./gocat join main.go
  ```

- **Join Multiple Files (including non-source files):**

  ```bash
  ./gocat join "main.go" "assets/*"
  ```

- **Join with Exclusions and Base Overrides:**

  ```bash
  ./gocat join "main.go" "./pkg/*.go" \
    -exclude-packages="expressions,lexer" \
    -exclude-files="vendor/*,testdata/*" \
    -java-base="com.example" \
    -go-base="github.com/example/project"
  ```

#### Additional Options

- `-exclude-packages`: Exclude Go files whose package declaration matches any of the specified comma-separated package names.
- `-exclude-files`: Exclude files matching any of the specified comma-separated glob patterns.
- `-java-base`: Specify the base package for Java/Kotlin recursive dependency resolution. If omitted, gocat will try to auto-detect the base package from common build files (`pom.xml`, `build.gradle`, or `build.gradle.kts`).
- `-go-base`: Specify the base module for Go recursive dependency resolution. This overrides reading the module name from `go.mod`.

#### Output Format

Each file in the output is wrapped in delimiters:

```
// --------- FILE START: "relative/path/to/file.go" (size: 1234 bytes, modtime: 2025-02-18T12:34:56Z) ----------
<file contents>
// --------- FILE END: "relative/path/to/file.go" ----------
```

### Split Command

The `split` command reads a bundled output (either from a file or STDIN) and recreates the original files based on the embedded delimiters.

#### Syntax

```bash
./gocat split [-in inputfile] [-out outputdirectory]
```

#### Options

- `-in`: Specifies the input file to split. If omitted, STDIN is used.
- `-out`: Specifies the output directory where the split files will be created. Defaults to the current directory if not provided.

#### Examples

- **Split from a File:**

  ```bash
  ./gocat split -in joined.txt -out outputFolder
  ```

- **Split Using STDIN:**

  ```bash
  ./gocat split -out outputFolder < joined.txt
  ```

### Help Command

The `help` command provides usage information for **gocat** and its subcommands.

#### Examples

- **General Help:**

  ```bash
  ./gocat help
  ```

- **Help for the Join Command:**

  ```bash
  ./gocat help join
  ```

- **Help for the Split Command:**

  ```bash
  ./gocat help split
  ```

## How It Works

1. **Module/Base Detection:**  
   - For Go files, gocat reads your module name from `go.mod` unless overridden by the `-go-base` flag.
   - For Java/Kotlin files, gocat determines the base package from common build files (`pom.xml`, `build.gradle`, or `build.gradle.kts`) unless explicitly provided via the `-java-base` flag.

2. **File Processing:**  
   - **Go Files:**  
     Each Go file specified (or matched via glob) is output with a header and footer delimiter. The file is parsed for its import statements, and for each import that starts with your module name, gocat locates the corresponding package directory and processes all Go files within that package recursively.
   - **Java/Kotlin Files:**  
     Each Java or Kotlin file is output with delimiters. The tool scans these files for import statements and, if an import belongs to the specified base package, recursively processes the corresponding source files.
   - **Non-Source Files:**  
     Files that do not have a supported source file extension are output with the same delimiters but are not further processed.

3. **Avoiding Duplicates:**  
   gocat tracks processed files (by their absolute paths) to ensure that each file is included only once, preventing infinite loops even if files import each other.

4. **Splitting:**  
   The `split` command reads the bundled output line-by-line, detects file boundaries using the embedded delimiters, and recreates each file in its original relative path.

## Limitations

- **Binary Files:**  
  gocat is primarily designed for source code and text files. Binary files may cause unexpected behavior if they contain lines matching the delimiter format.

- **Directory Traversal:**  
  Non-source files are not recursively traversed unless explicitly matched via glob patterns.

- **Delimiter Collisions:**  
  Ensure that your file contents do not inadvertently include lines matching the delimiter pattern.
  
