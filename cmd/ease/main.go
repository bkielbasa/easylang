// ease is the command-line tool for the Ease programming language.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-v", "--verbose":
			verbose = true
		case "-run":
			if i+1 < len(args) {
				pattern = args[i+1]
				i++
			}
		case "-h", "--help":
			fmt.Println(`Usage: ease test [options] [packages]

Options:
    -v           verbose output
    -run <pat>   run only tests matching pattern`)
			return
		}
	}

	// For now, just print that tests aren't implemented yet
	_ = verbose
	_ = pattern
	fmt.Println("ease test: test runner not yet implemented")
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

	// Fixup string addresses
	codeSize := uint64(emitter.CodeSize())
	if len(irProg.Strings) > 0 {
		stringOffsets := make([]uint64, len(irProg.Strings))
		offset := codeSize
		for i, s := range irProg.Strings {
			stringOffsets[i] = offset
			offset += uint64(len(s) + 1)
		}
		emitter.FixupStrings(stringOffsets)
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
