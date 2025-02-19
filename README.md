# gocat

**gocat** is a command-line utility written in Go that helps you bundle Go source files (and other related files) into a single output stream with embedded metadata delimiters. It also provides a way to split that stream back into the original files, preserving their relative paths. When processing Go files, gocat automatically reads your module name from `go.mod` and recursively includes any Go source files from packages within your module that are imported by the specified files. Any file that is not a Go source file is simply included as-is.

## Features

- **Recursive Go File Bundling:**  
  Reads the module name from `go.mod` and, for each provided Go file (or glob pattern), outputs the file with a header and footer delimiter. It then parses the file for import statements and recursively includes all Go files from packages that belong to the same module.

- **Non-Go File Inclusion:**  
  Any file that does not have a `.go` extension is output with the same delimiters but is not further processed.

- **No Duplicate Inclusions:**  
  Each file is processed only once, even if it is referenced multiple times.

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

- Go 1.16 or later

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

The `join` command reads one or more files or glob patterns and outputs the contents of each file wrapped in header and footer delimiters. For Go files, it also parses import statements to recursively include Go files from your module.

#### Syntax

```bash
./gocat join [file or glob pattern] ...
```

#### Examples

- **Join a Single Go File:**

  ```bash
  ./gocat join main.go
  ```

- **Join Multiple Files (including non-Go files):**

  ```bash
  ./gocat join "main.go" "assets/*"
  ```

- **Join with Exclusions:**

  ```bash
  ./gocat join "main.go" "./pkg/*.go" -exclude-packages="expressions,lexer" -exclude-files="vendor/*,testdata/*"
  ```

#### Additional Options

- `-exclude-packages`: Exclude Go files whose package declaration matches any of the specified comma-separated package names.
- `-exclude-files`: Exclude files matching any of the specified comma-separated glob patterns.

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

1. **Module Detection:**  
   gocat reads your module name from `go.mod` in the current directory. This information is used to determine which import paths belong to your module.

2. **File Processing:**  
   - **Go Files:**  
     Each Go file specified (or matched via glob) is output with a header and footer delimiter. The file is parsed for its import statements, and for each import that starts with your module name, gocat locates the corresponding package directory and processes all Go files within that package recursively.
   - **Non-Go Files:**  
     Files that do not have a `.go` extension are output with the same delimiters but without further processing.

3. **Avoiding Duplicates:**  
   gocat tracks processed files to ensure that each file is included only once.

4. **Splitting:**  
   The `split` command reads the bundled output line-by-line, detects file boundaries using the delimiters, and recreates each file in its original relative path.

## Limitations

- **Binary Files:**  
  gocat is primarily designed for source code and text files. Binary files may cause unexpected behavior if they contain lines matching the delimiter format.

- **Directory Traversal:**  
  Non-Go files are not recursively traversed unless explicitly matched via glob patterns.

- **Delimiter Collisions:**  
  Ensure that your file contents do not inadvertently include lines matching the delimiter pattern.
