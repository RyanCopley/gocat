Below is a formal specification document for **gocat**. This document describes the intended behavior, input/output formats, algorithms, error handling, and all other aspects of the program in exhaustive detail.

---

# Gocat Formal Specification

**Version:** 1.0  
**Date:** 2025-02-18  
**Author:** Ryan Copley

---

## 1. Overview

**Gocat** is a command-line tool written in Go that bundles multiple files into a single output stream and can later split that stream back into the original files. It is designed primarily for Go source files but also supports non-Go files. For Go files, gocat recursively follows internal module dependencies (as defined in the `go.mod` file) and includes all associated files. The output uses clearly defined delimiters and begins with a mandatory "magic header" to identify the file format and version.

---

## 2. Purpose and Scope

### 2.1 Purpose

- **Bundling:** To combine multiple Go source files and other related files (e.g., configuration files, templates) into a single stream.  
- **Dependency Resolution:** To analyze Go files for import statements that reference packages within the same module (as determined by `go.mod`), and recursively include all such Go files.  
- **Splitting:** To reconstruct the original file hierarchy from a bundled stream by parsing the embedded delimiters.

### 2.2 Scope

- **Input:** Files and glob patterns specified on the command line.
- **Processing:** 
  - For Go source files (`*.go`):  
    - Read file contents.
    - Parse for import statements using Go’s parser (imports only).
    - For each import that begins with the module name (as read from `go.mod`), compute the corresponding package directory and process every file in that directory (non-recursively).
  - For non-Go files:  
    - Simply include the file with no further recursive processing.
- **Output:** A single concatenated text stream with:
  - A mandatory "magic header" as the first line.
  - Each file is wrapped in header and footer delimiters containing metadata.
- **Splitting:** The tool must verify the magic header and then recreate each file using the delimiters.

---

## 3. Terminology and Definitions

- **Gocat:** The name of the program.
- **Magic Header:** A mandatory first line in the bundled output that identifies the file as produced by gocat.  
  **Format:**  
  ```
  // --------- gocat v1
  ```
- **File Delimiter:** Special lines inserted before and after each file’s contents.  
  - **Header Delimiter:**  
    ```
    // --------- FILE START: "relative/path/to/file" (size: X bytes, modtime: TIMESTAMP) ----------
    ```
  - **Footer Delimiter:**  
    ```
    // --------- FILE END: "relative/path/to/file" ----------
    ```
- **Module Name:** The Go module’s identifier as defined in the `go.mod` file.
- **Processed File:** Any file that has already been included in the bundle (tracked by its absolute path) to avoid duplicate inclusion.

---

## 4. System Architecture

Gocat is structured as a command-line tool with three primary subcommands:
1. **join:** Bundles files (and recursively includes Go dependencies) into one stream.
2. **split:** Splits a bundled file back into the original files using the delimiters.
3. **help:** Displays usage information.

The program uses standard Go libraries for file operations, flag parsing, and Go source parsing (via `go/parser`).

---

## 5. Functional Requirements

### 5.1 General Requirements

- **Startup:**  
  - When invoked, gocat must validate that at least one subcommand is provided.  
  - For the `join` command, it must output the magic header as the very first line.
- **Subcommand Handling:**  
  - The program shall support the following subcommands:
    - `join`
    - `split`
    - `help`
- **Error Reporting:**  
  - Errors must be printed to standard error.
  - The program should exit with a non-zero status code upon encountering a fatal error (e.g., missing `go.mod`, invalid magic header on split).

### 5.2 Join Command Requirements

- **Command Syntax:**  
  ```
  gocat join [file or glob pattern] ...
  ```
- **Module Resolution:**  
  - Gocat must read the `go.mod` file in the current working directory.
  - It shall extract the module name from a line starting with `module `.
  - If `go.mod` is missing or the module name is not found, the program must exit with an error.
- **File Processing:**  
  - For each file (expanded from a glob pattern):
    - **Go Files (`*.go`):**
      - Output the file’s contents enclosed within header and footer delimiters.
      - Parse the file (using `go/parser` with `parser.ImportsOnly`) to extract import paths.
      - For each import whose path starts with the module name:
        - Compute the corresponding package directory.
        - Enumerate all files in that directory (non-recursively).
        - Process each file (Go or non-Go) recursively if it has not been processed already.
    - **Non-Go Files:**
      - Output the file’s contents enclosed within header and footer delimiters.
      - No further processing (i.e., no recursive dependency analysis) is performed.
  - **Duplication Avoidance:**  
    - The tool shall maintain a set (e.g., a map keyed by the absolute file path) to ensure that no file is processed more than once.
- **Output Format:**  
  - The very first line must be the magic header:  
    ```
    // --------- gocat v1
    ```
  - Each file is then output as follows:
    - **Header Delimiter:**  
      ```
      // --------- FILE START: "relative/path/to/file" (size: X bytes, modtime: TIMESTAMP) ----------
      ```
    - **File Contents:**  
      - The raw contents of the file.
    - **Footer Delimiter:**  
      ```
      // --------- FILE END: "relative/path/to/file" ----------
      ```

### 5.3 Split Command Requirements

- **Command Syntax:**  
  ```
  gocat split [-in inputfile] [-out outputdirectory]
  ```
- **Input Handling:**  
  - If the `-in` flag is provided, the program shall read from the specified file; otherwise, it shall read from standard input.
- **Magic Header Validation:**  
  - The first line of the input must exactly match the expected magic header (`// --------- gocat v1`).
  - If the magic header is missing or invalid, the program must abort with an error.
- **File Extraction:**  
  - The program shall scan the input line-by-line.
  - Upon detecting a header delimiter:
    - Extract the file name (enclosed in double quotes).
    - Create the output file at the location specified by the file name, using the `-out` directory as the base (if provided).
    - Recreate the necessary directory structure.
  - Continue reading lines until a footer delimiter for the current file is detected.
  - Write the intervening lines (appending newline characters as necessary) to the output file.
  - Once the footer is encountered, close the current file and resume scanning.
- **Error Handling During Splitting:**  
  - If any delimiter is malformed (e.g., missing quotes) or file I/O errors occur, log an error message and continue processing if possible.
  - The split operation should report errors but process as many files as possible.

### 5.4 Help Command Requirements

- **Command Syntax:**  
  ```
  gocat help [subcommand]
  ```
- **Behavior:**  
  - With no additional argument, the help command shall display general usage instructions and list available subcommands.
  - When a subcommand (e.g., `join` or `split`) is specified, display detailed help for that subcommand.

---

## 6. Non-Functional Requirements

- **Performance:**  
  - The tool should efficiently handle a moderate number of files (tens to hundreds) without noticeable delay.
- **Portability:**  
  - gocat must compile and run on all platforms that support Go.
- **Maintainability:**  
  - The source code shall be modular, with clear separation between file processing, CLI argument handling, and splitting logic.
- **Usability:**  
  - Clear and detailed help messages must be provided.
  - Error messages should be informative.

---

## 7. Detailed Design

### 7.1 Data Structures

- **Processed Files Map:**  
  A map of type `map[string]bool` (keyed by absolute file path) to track which files have been processed.
- **File Delimiter Strings:**  
  - **Magic Header:** A constant string:  
    ```
    "// --------- gocat v1"
    ```
  - **File Start Delimiter:**  
    Begins with:  
    ```
    "// --------- FILE START: "
    ```
    Followed by a quoted file name, metadata (size and modification time), and a trailing marker (`----------`).
  - **File End Delimiter:**  
    Begins with:  
    ```
    "// --------- FILE END: "
    ```
    Followed by a quoted file name and the trailing marker.

### 7.2 Algorithms

#### 7.2.1 Join Command Algorithm

1. **Print Magic Header:**  
   - Output the line:  
     ```
     // --------- gocat v1
     ```
2. **Read Module Name:**  
   - Open `go.mod`.
   - Find a line beginning with `module `.
   - Extract the module name.
3. **Process Each Argument:**  
   - For each file or glob pattern provided:
     1. Expand the glob pattern.
     2. For each matching file:
        - Determine if the file has already been processed (using its absolute path).
        - If not processed:
          - If the file extension is `.go`, invoke the Go file processing routine.
          - Otherwise, invoke the non-Go file processing routine.
4. **Processing a Go File:**  
   - Open and read the file.
   - Compute the file’s relative path (relative to the current working directory).
   - Output the header delimiter with metadata (file size, modification time).
   - Output the file’s contents.
   - Output the footer delimiter.
   - Re-open the file to parse for import statements.
   - For each import:
     - Unquote the import path.
     - If the import path begins with the module name:
       - Compute the relative package directory.
       - Enumerate all files (non-recursively) in that directory.
       - For each file in the directory, recursively process it using the same algorithm.
5. **Processing a Non-Go File:**  
   - Similar to Go files but without parsing imports.
   - Open, output header, file contents, and footer.

#### 7.2.2 Split Command Algorithm

1. **Validate Magic Header:**  
   - Read the first line from input.
   - If it does not match the magic header (`// --------- gocat v1`), abort with an error.
2. **Scan Input Line-By-Line:**  
   - For each subsequent line:
     - If the line begins with the header delimiter:
       - Parse and extract the file name (enclosed in quotes).
       - If an output directory is specified, prepend it to the file name.
       - Create any necessary directories.
       - Open a new output file for writing.
       - Set a flag to indicate that file content should now be written.
     - Else if the line begins with the footer delimiter (and if file content is being written):
       - Close the output file.
       - Clear the flag.
     - Otherwise, if the flag is set, write the line (plus a newline) to the output file.
3. **Error Conditions:**  
   - If a header or footer delimiter is malformed, log an error and continue.
   - If file I/O operations fail, log an error and attempt to process subsequent files.

### 7.3 Command-Line Interface (CLI)

- **Usage:**  
  ```
  gocat <command> [options] [arguments]
  ```
- **Subcommands:**
  - `join` – Bundles files into a single stream.
  - `split` – Splits a bundled stream into individual files.
  - `help` – Displays usage information.

- **Join Example:**  
  ```
  gocat join "main.go" "assets/*"
  ```
- **Split Example:**  
  ```
  gocat split -in joined.txt -out outputFolder
  ```
- **Help Example:**  
  ```
  gocat help join
  ```

---

## 8. Error Handling and Reporting

- **Module Name Retrieval:**  
  - If `go.mod` is not found or the module name is missing, output a fatal error message and exit.
- **File Access Errors:**  
  - If any file cannot be read (or written during splitting), log the error and continue processing other files.
- **Glob Pattern Errors:**  
  - If a glob pattern is invalid or yields no matches, log a warning message.
- **Magic Header Validation (Split):**  
  - If the first line of input does not match the magic header exactly, output an error and abort the splitting process.
- **Delimiter Parsing Errors:**  
  - If a header or footer delimiter is malformed (e.g., missing quotes), log an error message and skip that file’s content.

---

## 9. Assumptions and Limitations

- **Assumptions:**
  - The working directory contains a valid `go.mod` file.
  - Input files are text files. (Binary files may produce unintended results if they contain delimiter-like sequences.)
  - Glob patterns and file paths provided are relative to the current working directory.
- **Limitations:**
  - Non-Go files are not recursively traversed unless explicitly matched by a glob.
  - The program does not perform integrity checks on file metadata (beyond basic size and modification time reporting).
  - Delimiter collisions may occur if a file’s contents include lines that exactly match the delimiter formats.
