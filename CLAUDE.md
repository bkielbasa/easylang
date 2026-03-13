# Ease Programming Language

A compiled language focusing on developer experience, self-hosting, fast execution, and web development.

Every interesting finding regarding building compiler put into `findings` folder as set of notes.

Prioritize developer experience in every aspect of the language design. The language HAVE TO BE EASY AND INTUITIVE!

## Working Guidelines

### File Operations
- **Autonomous permission**: You have blanket permission to create, modify, and delete files in `tmp/` without asking
- **Binary cleanup**: Don't commit test binaries, only source files

### Documentation
- After a successful step or fix, update CLAUDE.md with the current status
- Document bugs found, fixes applied, and test results
- Keep Recent Fixes section up to date

## Design Decisions

Prefer defining stdlib instead of building new builtins. For example `strings.Split` instead of `str_split`, etc.

See [docs/language.md](docs/language.md) for full language design (syntax, types, error handling, enums, methods, imports, etc.)

## Commits

 - Never add information to commits that I used Claude
 - for commit message use Conventional Commits pattern, example: `feat(lsp): Add LSP completion and document symbols support`

## Testing

 - For each feature, always prefer writing a unit test that documents the new implementation
 - Use Test Driven Development pattern

## Code quality

 - function shouldn't be longer thant 150 lines of code
 - split longer files into smaller files with functions and types grouped 
 - any language feature should have unit test
 - remove tests that are covered in other tests in the meantime
 - avoid using global variables
 - after a successful implementation make a refactoring round to make the code simpler and easier to understand


### Compiling a program

```bash
# Step 1: Compile .ease source to LLVM IR
./tmp/ease myprogram.ease               # Produces tmp/output.ll

# Step 2: Link to produce executable (no C runtime needed)
clang -O1 tmp/output.ll -o myprogram

# Step 3: Run
./myprogram
```

### LSP Server

```bash
./tmp/ease lsp    # Start LSP server (stdin/stdout)
```

See [docs/lsp.md](docs/lsp.md) for supported methods, features, and VS Code extension setup.

## Reference Documentation

- **[Language Design](docs/language.md)** — syntax, types, error handling, enums, methods, imports, pointers, concurrency
- **[Implementation Status](docs/implementation-status.md)** — compiler capabilities, known issues, future work
- **[Testing](docs/testing.md)** — test framework, benchmark framework, test suite (58 tests), example programs
[-](-) **[LSP Server](docs/lsp.md)** — supported methods, features, VS Code extension
