// ease is the command-line tool for the Ease programming language.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ease/pkg/ast"
	"ease/pkg/codegen/arm64"
	"ease/pkg/ir"
	"ease/pkg/lexer"
	"ease/pkg/macho"
	"ease/pkg/parser"
	"ease/pkg/sema"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "build":
		cmdBuild(args)
	case "run":
		cmdRun(args)
	case "test":
		cmdTest(args)
	case "version":
		fmt.Printf("ease version %s\n", version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "ease: unknown command %q\n", cmd)
		fmt.Fprintln(os.Stderr, "Run 'ease help' for usage.")
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Ease is a tool for managing Ease source code.

Usage:

    ease <command> [arguments]

Commands:

    build       compile source files
    run         compile and run program
    test        run tests
    version     print version

Use "ease <command> -h" for more information about a command.`)
}

// cmdBuild compiles source files to an executable.
func cmdBuild(args []string) {
	var output string
	var verbose bool
	var dumpIR bool

	// Simple flag parsing
	var files []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 < len(args) {
				output = args[i+1]
				i++
			}
		case "-v", "--verbose":
			verbose = true
		case "--dump-ir":
			dumpIR = true
		case "-h", "--help":
			fmt.Println(`Usage: ease build [options] <file.ease>

Options:
    -o <file>    output file name (default: name of input without extension)
    -v           verbose output
    --dump-ir    dump IR to stdout`)
			return
		default:
			if !strings.HasPrefix(args[i], "-") {
				files = append(files, args[i])
			}
		}
	}

	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "ease build: no input files")
		os.Exit(1)
	}

	inputFile := files[0]

	// Default output name: input file without extension
	if output == "" {
		base := filepath.Base(inputFile)
		output = strings.TrimSuffix(base, filepath.Ext(base))
	}

	if err := compile(inputFile, output, verbose, dumpIR); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Sign the binary (required on macOS)
	signCmd := exec.Command("codesign", "-s", "-", "-f", output)
	signCmd.Stderr = os.Stderr
	if err := signCmd.Run(); err != nil {
		// Non-fatal warning
		if verbose {
			fmt.Fprintf(os.Stderr, "warning: codesign failed: %v\n", err)
		}
	} else if verbose {
		fmt.Println("Binary signed with ad-hoc signature")
	}
}

// cmdRun compiles and runs the program.
func cmdRun(args []string) {
	var verbose bool
	var dumpIR bool

	var files []string
	var progArgs []string
	seenDash := false

	for i := 0; i < len(args); i++ {
		if seenDash {
			progArgs = append(progArgs, args[i])
			continue
		}
		switch args[i] {
		case "-v", "--verbose":
			verbose = true
		case "--dump-ir":
			dumpIR = true
		case "--":
			seenDash = true
		case "-h", "--help":
			fmt.Println(`Usage: ease run [options] <file.ease> [-- args...]

Options:
    -v           verbose output
    --dump-ir    dump IR to stdout

Arguments after -- are passed to the program.`)
			return
		default:
			if !strings.HasPrefix(args[i], "-") {
				files = append(files, args[i])
			}
		}
	}

	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "ease run: no input files")
		os.Exit(1)
	}

	inputFile := files[0]

	// Compile to temp file
	tmpFile := filepath.Join(os.TempDir(), "ease-run-"+filepath.Base(inputFile))
	defer os.Remove(tmpFile)

	if err := compile(inputFile, tmpFile, verbose, dumpIR); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Sign the binary (required on macOS)
	signCmd := exec.Command("codesign", "-s", "-", "-f", tmpFile)
	signCmd.Stderr = os.Stderr
	if err := signCmd.Run(); err != nil {
		// Non-fatal, might work without signing on some systems
		if verbose {
			fmt.Fprintf(os.Stderr, "warning: codesign failed: %v\n", err)
		}
	}

	// Run the program
	cmd := exec.Command(tmpFile, progArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "ease run: %v\n", err)
		os.Exit(1)
	}
}

// cmdTest runs tests.
func cmdTest(args []string) {
	var verbose bool
	var pattern string
	var tags []string
	var skipTags []string
	var testDir string = "."

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-v", "--verbose":
			verbose = true
		case "-run", "-name":
			if i+1 < len(args) {
				pattern = args[i+1]
				i++
			}
		case "-tag":
			if i+1 < len(args) {
				tags = append(tags, args[i+1])
				i++
			}
		case "-skip":
			if i+1 < len(args) {
				skipTags = append(skipTags, args[i+1])
				i++
			}
		case "-h", "--help":
			fmt.Println(`Usage: ease test [options] [directory]

Options:
    -v            verbose output
    -name <pat>   run only tests with descriptions matching pattern
    -tag <tag>    run only tests with this tag (e.g., slow, integration)
    -skip <tag>   skip tests with this tag

Examples:
    ease test                    # run all tests in current directory
    ease test ./mypackage        # run tests in specific directory
    ease test -name "login"      # run tests matching "login"
    ease test -tag slow          # run only slow tests
    ease test -skip integration  # skip integration tests`)
			return
		default:
			if !strings.HasPrefix(args[i], "-") {
				testDir = args[i]
			}
		}
	}

	// Find all *_test.ease files
	testFiles, err := findTestFiles(testDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ease test: %v\n", err)
		os.Exit(1)
	}

	if len(testFiles) == 0 {
		fmt.Println("ease test: no test files found")
		return
	}

	if verbose {
		fmt.Printf("Found %d test file(s)\n", len(testFiles))
	}

	// Collect all tests
	var allTests []testCase
	for _, file := range testFiles {
		tests, err := collectTests(file, pattern, tags, skipTags)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ease test: error in %s: %v\n", file, err)
			continue
		}
		allTests = append(allTests, tests...)
	}

	if len(allTests) == 0 {
		fmt.Println("ease test: no tests to run")
		return
	}

	// Run tests
	passed := 0
	failed := 0

	for _, tc := range allTests {
		if verbose {
			fmt.Printf("=== RUN   %s\n", tc.name)
		}

		err := runTest(tc, verbose)
		if err != nil {
			failed++
			fmt.Printf("--- FAIL: %s\n", tc.name)
			if verbose {
				fmt.Printf("    %v\n", err)
			}
		} else {
			passed++
			if verbose {
				fmt.Printf("--- PASS: %s\n", tc.name)
			}
		}
	}

	// Summary
	fmt.Println()
	if failed > 0 {
		fmt.Printf("FAIL: %d passed, %d failed\n", passed, failed)
		os.Exit(1)
	}
	fmt.Printf("PASS: %d passed\n", passed)
}

type testCase struct {
	name       string
	file       string
	sourceCode string // complete program source for this test
}

// findTestFiles finds all *_test.ease files in a directory
func findTestFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, "_test.ease") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// collectTests parses a file and collects test cases
func collectTests(file string, pattern string, tags, skipTags []string) ([]testCase, error) {
	source, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	l := lexer.New(string(source), file)
	p := parser.New(l)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("parse errors: %v", p.Errors())
	}

	var tests []testCase

	// Collect non-test declarations (functions, structs, etc.)
	var nonTestDecls []string
	sourceLines := strings.Split(string(source), "\n")

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *ast.TestDecl:
			// Check if test matches pattern
			if pattern != "" && !strings.Contains(d.Description.Value, pattern) {
				continue
			}

			// Check tags
			testTags := getTestTags(d.Attributes)
			if len(tags) > 0 && !hasAnyTag(testTags, tags) {
				continue
			}
			if hasAnyTag(testTags, skipTags) {
				continue
			}

			// Extract test body source
			testBody := extractTestBody(sourceLines, d)

			// Build complete program for this test
			testProg := buildTestProgram(sourceLines, prog, d, testBody)

			tests = append(tests, testCase{
				name:       d.Description.Value,
				file:       file,
				sourceCode: testProg,
			})

		default:
			// Track non-test declarations for context
			_ = nonTestDecls
		}
	}

	return tests, nil
}

// getTestTags extracts tag names from test attributes
func getTestTags(attrs []ast.Attribute) []string {
	var tags []string
	for _, attr := range attrs {
		tags = append(tags, attr.Name.Name)
	}
	return tags
}

// hasAnyTag checks if testTags contains any of the target tags
func hasAnyTag(testTags, targetTags []string) bool {
	for _, tt := range testTags {
		for _, target := range targetTags {
			if tt == target {
				return true
			}
		}
	}
	return false
}

// extractTestBody extracts the body of a test declaration from source
func extractTestBody(lines []string, test *ast.TestDecl) string {
	// Get start position from the block's Token
	startLine := test.Body.Token.Pos.Line - 1 // 0-indexed

	if startLine < 0 || startLine >= len(lines) {
		return ""
	}

	// Find the end by counting braces
	var body strings.Builder
	braceCount := 0
	started := false
	firstLine := true

	for i := startLine; i < len(lines); i++ {
		line := lines[i]
		if firstLine {
			// On the first line, find the opening brace and skip everything before it
			braceIdx := strings.Index(line, "{")
			if braceIdx >= 0 {
				started = true
				braceCount = 1
				// Write content after the brace if any
				rest := strings.TrimSpace(line[braceIdx+1:])
				if rest != "" {
					body.WriteString("    ")
					body.WriteString(rest)
					body.WriteString("\n")
				}
			}
			firstLine = false
			continue
		}

		// Count braces in the line
		for j, ch := range line {
			if ch == '{' {
				braceCount++
			} else if ch == '}' {
				braceCount--
				if started && braceCount == 0 {
					// Found closing brace, include text before it
					content := strings.TrimRight(line[:j], " \t")
					if content != "" {
						body.WriteString(content)
					}
					return body.String()
				}
			}
		}

		// Add line to body
		if started && braceCount > 0 {
			body.WriteString(line)
			body.WriteString("\n")
		}
	}

	return body.String()
}

// buildTestProgram builds a complete program source that runs a single test
func buildTestProgram(lines []string, prog *ast.Program, test *ast.TestDecl, testBody string) string {
	var builder strings.Builder

	// Copy all non-test declarations (structs, functions, etc.)
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *ast.TestDecl:
			// Skip test declarations
			continue
		default:
			// Extract source for this declaration
			startLine := d.Pos().Line - 1
			endLine := findDeclEnd(lines, startLine)
			for i := startLine; i <= endLine && i < len(lines); i++ {
				builder.WriteString(lines[i])
				builder.WriteString("\n")
			}
			builder.WriteString("\n")
		}
	}

	// Generate main function that runs the test
	builder.WriteString("fn main() -> int {\n")
	builder.WriteString(testBody)
	builder.WriteString("\n    return 0\n")
	builder.WriteString("}\n")

	return builder.String()
}

// findDeclEnd finds the end line of a declaration starting at startLine
func findDeclEnd(lines []string, startLine int) int {
	braceCount := 0
	inDecl := false

	for i := startLine; i < len(lines); i++ {
		line := lines[i]
		for _, ch := range line {
			if ch == '{' {
				braceCount++
				inDecl = true
			} else if ch == '}' {
				braceCount--
				if inDecl && braceCount == 0 {
					return i
				}
			}
		}
	}
	return startLine
}

// runTest compiles and runs a single test
func runTest(tc testCase, verbose bool) error {
	// Write test program to temp file
	tmpDir, err := os.MkdirTemp("", "ease-test-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	srcFile := filepath.Join(tmpDir, "test.ease")
	if err := os.WriteFile(srcFile, []byte(tc.sourceCode), 0644); err != nil {
		return err
	}

	binFile := filepath.Join(tmpDir, "test")

	// Compile
	if err := compile(srcFile, binFile, false, false); err != nil {
		return fmt.Errorf("compile error: %v", err)
	}

	// Sign (macOS requirement)
	signCmd := exec.Command("codesign", "-s", "-", "-f", binFile)
	signCmd.Run() // Ignore errors

	// Run
	cmd := exec.Command(binFile)
	output, err := cmd.CombinedOutput()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			if code != 0 {
				// Non-zero exit = test failure
				if len(output) > 0 {
					return fmt.Errorf("exit code %d: %s", code, string(output))
				}
				return fmt.Errorf("exit code %d", code)
			}
		}
		return err
	}

	return nil
}

// compile compiles an Ease source file to a binary.
func compile(inputFile, output string, verbose, dumpIR bool) error {
	// Read source file
	source, err := os.ReadFile(inputFile)
	if err != nil {
		return fmt.Errorf("error reading %s: %v", inputFile, err)
	}

	if verbose {
		fmt.Printf("Compiling %s...\n", inputFile)
	}

	// Lexer
	l := lexer.New(string(source), inputFile)

	// Parser
	p := parser.New(l)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		fmt.Fprintln(os.Stderr, "Parse errors:")
		for _, e := range p.Errors() {
			fmt.Fprintln(os.Stderr, " ", e)
		}
		return fmt.Errorf("compilation failed")
	}

	if verbose {
		fmt.Printf("Parsed %d declarations\n", len(prog.Decls))
	}

	// Semantic analysis
	analyzer := sema.New()
	typeInfo, semaErrors := analyzer.Analyze(prog)

	if len(semaErrors) > 0 {
		fmt.Fprintln(os.Stderr, "Semantic errors:")
		for _, e := range semaErrors {
			fmt.Fprintln(os.Stderr, " ", e)
		}
		return fmt.Errorf("compilation failed")
	}

	if verbose {
		fmt.Println("Semantic analysis complete")
	}

	// IR generation
	builder := ir.NewBuilder(typeInfo)
	irProg := builder.Build(prog)

	if dumpIR {
		fmt.Println("=== IR ===")
		fmt.Println(irProg)
		fmt.Println("==========")
	}

	if verbose {
		fmt.Printf("Generated IR for %d functions\n", len(irProg.Functions))
	}

	// Code generation
	emitter := arm64.NewEmitter()

	// Track function offsets for the linker
	funcOffsets := make(map[string]int64)

	// Emit all functions
	emitter.EmitProgram(irProg, funcOffsets)

	if verbose {
		for name, offset := range funcOffsets {
			fmt.Printf("  Function %s at offset %d\n", name, offset)
		}
	}

	// Find main function offset
	mainOff, hasMain := funcOffsets["main"]
	if !hasMain {
		return fmt.Errorf("error: no main function found")
	}

	// Calculate memory layout for fixups
	const (
		baseAddr = 0x100000000 // Base VM address
		pageSize = 0x4000      // 16KB pages on Apple Silicon
	)

	codeSize := uint64(emitter.CodeSize())

	// Calculate code VM address (will be at some offset after headers)
	// For now, estimate ~1KB for headers and load commands
	headerSize := uint64(1024)
	codeFileOff := (headerSize + 255) & ^uint64(255) // Align to 256 bytes
	codeVMAddr := baseAddr + codeFileOff

	// Fixup string addresses (strings are in __TEXT segment after code)
	if len(irProg.Strings) > 0 {
		stringOffsets := make([]uint64, len(irProg.Strings))
		offset := codeSize
		for i, s := range irProg.Strings {
			stringOffsets[i] = offset
			offset += uint64(len(s) + 1)
		}
		emitter.FixupStrings(stringOffsets)
	}

	// Calculate global VM addresses (globals are in __DATA segment)
	if len(irProg.GlobalVars) > 0 {
		// Calculate strings size
		stringsSize := uint64(0)
		for _, s := range irProg.Strings {
			stringsSize += uint64(len(s) + 1)
		}

		// __TEXT segment size (code + strings, page-aligned)
		textSegSize := ((codeFileOff + codeSize + stringsSize + pageSize - 1) / pageSize) * pageSize

		// __DATA segment starts at next page boundary
		dataVMAddr := baseAddr + textSegSize

		// Calculate each global's VM address
		globalAddrs := make(map[string]uint64)
		offset := 0
		for _, gv := range irProg.GlobalVars {
			// Align to 8-byte boundary
			if offset%8 != 0 {
				offset += 8 - (offset % 8)
			}
			globalAddrs[gv.Name] = dataVMAddr + uint64(offset)
			offset += gv.Size
		}

		// Fixup global variable addresses
		emitter.FixupGlobals(globalAddrs, codeVMAddr)
	}

	code := emitter.Code()

	if verbose {
		fmt.Printf("Generated %d bytes of ARM64 code\n", len(code))
		if len(irProg.Strings) > 0 {
			fmt.Printf("  %d string constants\n", len(irProg.Strings))
		}
		fmt.Printf("main function at offset %d\n", mainOff)
	}

	// Mach-O generation
	writer := macho.NewWriter()
	writer.SetCode(code)
	writer.SetStrings(irProg.Strings)
	writer.SetMainOffset(mainOff)

	// Set global variables with calculated offsets
	if len(irProg.GlobalVars) > 0 {
		machoGlobals := make([]macho.GlobalVar, 0, len(irProg.GlobalVars))
		offset := 0
		for _, gv := range irProg.GlobalVars {
			// Align to 8-byte boundary
			if offset%8 != 0 {
				offset += 8 - (offset % 8)
			}

			// Extract initial value for simple types
			var initVal int64 = 0
			if gv.InitVal != nil {
				switch val := gv.InitVal.(type) {
				case *ast.IntLit:
					initVal = val.Value
				case *ast.BoolLit:
					if val.Value {
						initVal = 1
					}
				}
			}

			machoGlobals = append(machoGlobals, macho.GlobalVar{
				Name:   gv.Name,
				Offset: offset,
				Size:   gv.Size,
				Value:  initVal,
			})
			offset += gv.Size
		}
		writer.SetGlobalVars(machoGlobals)
	}

	// Add symbols
	for name, offset := range funcOffsets {
		extern := name == "main"
		writer.AddSymbol("_"+name, offset, 1, extern)
	}

	binary := writer.Write()

	// Write output file
	if err := os.WriteFile(output, binary, 0755); err != nil {
		return fmt.Errorf("error writing %s: %v", output, err)
	}

	if verbose {
		fmt.Printf("Wrote %d bytes to %s\n", len(binary), output)
	}

	return nil
}
