# LSP Server

The Ease compiler includes a built-in LSP server for IDE integration.

```bash
# Start the LSP server (communicates over stdin/stdout)
./tmp/ease lsp
```

## Features

**Phase 1 (implemented)**: Diagnostics — reports syntax errors on file open/change.
**Phase 2 (implemented)**: Go-to-definition — jump to within-file definitions of functions, structs, enums, and global variables.
**Phase 2.5 (implemented)**: Hover — show function signatures, struct/enum definitions on hover (`K` in Neovim).
**Phase 3 (implemented)**: Completion — autocomplete function/struct/enum/global names as you type.
**Phase 3.5 (implemented)**: Document Symbols — outline view (Ctrl+Shift+O in VS Code, `:Telescope lsp_document_symbols` in Neovim).

## Supported LSP Methods

- `initialize` — returns capabilities (textDocumentSync=Full, definitionProvider, hoverProvider, completionProvider, documentSymbolProvider)
- `textDocument/didOpen` — parse and publish diagnostics, cache source
- `textDocument/didChange` — re-parse and publish diagnostics, update cache
- `textDocument/didSave` — re-parse if text included
- `textDocument/didClose` — clear diagnostics
- `textDocument/definition` — go-to-definition for functions, structs, enums, globals (within-file)
- `textDocument/hover` — show signatures for functions, structs, enums, globals (within-file)
- `textDocument/completion` — autocomplete function/struct/enum/global names with prefix filtering
- `textDocument/documentSymbol` — outline of all top-level declarations with symbol kinds
- `shutdown` / `exit` — clean shutdown

## VS Code Extension

Located at `editors/vscode/`.

```bash
cd editors/vscode
npm install
npm run compile
# Launch VS Code with the extension:
code --extensionDevelopmentPath=.
```

Set `ease.serverPath` in VS Code settings to point to your `ease` binary.
