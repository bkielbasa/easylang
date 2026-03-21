# OS Package Expansion — Design

## Goal

Expand the `os` stdlib package from 6 to 19 functions covering environment variables, process control, file writing, and filesystem operations. Use a Rust-inspired flat `OsResult` enum for error handling with structured error variants mapped from errno.

## Current State

The `os` package (`bootstrap/ease/os/os.ease`) has 6 functions:
- `System(cmd: string) -> int` — shell command execution via `extern fn system`
- `ReadFile(path: string) -> string` — read file using syscall primitives
- `IsDir(path: string) -> int` — LLVM helper using `stat()`
- `ListDir(path: string) -> string` — LLVM helper using `opendir/readdir`
- `Argc() -> int` — IR opcode
- `Argv(index: int) -> string` — IR opcode

Two implementation approaches exist:
1. **`extern fn` FFI** — declare C functions, auto-generated LLVM wrappers handle type bridging
2. **LLVM helper functions** — for operations needing C struct access (stat, dirent)

## Design

### OsResult Enum

A flat enum combining success and structured error variants:

```ease
enum OsResult {
    Ok(int),
    NotExist,
    Permission,
    AlreadyExists,
    IsDir,
    NotDir,
    Err(string),
}
```

Errno mapping:
- `ENOENT` (2) -> `NotExist`
- `EACCES` (13) -> `Permission`
- `EEXIST` (17) -> `AlreadyExists`
- `EISDIR` (21) -> `IsDir`
- `ENOTDIR` (20) -> `NotDir`
- Anything else -> `Err(strerror(errno))`

### Errno Access

On macOS, errno is thread-local and accessed via `__error()`:

```ease
extern fn __error() -> ptr          // returns pointer to errno
extern fn strerror(errnum: i32) -> ptr  // errno -> human-readable string
```

Helper function to read errno and build OsResult:

```ease
fn get_errno() -> int {
    errptr := __error()
    return peek(errptr)
}

fn make_error() -> OsResult {
    e := get_errno()
    if e == 2 { return NotExist }
    if e == 13 { return Permission }
    if e == 17 { return AlreadyExists }
    if e == 21 { return IsDir }
    if e == 20 { return NotDir }
    msg := strerror(e)
    return Err(msg)
}
```

### New Functions (13 total)

#### Environment (3 functions)

```ease
extern fn getenv(name: ptr) -> ptr
extern fn setenv(name: ptr, value: ptr, overwrite: i32) -> i32
extern fn unsetenv(name: ptr) -> i32

fn Getenv(name: string) -> string {
    result := getenv(name)
    if result == 0 { return "" }
    return result
}

fn Setenv(name: string, value: string) -> OsResult {
    rc := setenv(name, value, 1)
    if rc != 0 { return make_error() }
    return Ok(0)
}

fn Unsetenv(name: string) -> OsResult {
    rc := unsetenv(name)
    if rc != 0 { return make_error() }
    return Ok(0)
}
```

#### Process (2 functions)

```ease
extern fn exit(code: i32)
extern fn getpid() -> i32

fn Exit(code: int) {
    exit(code)
}

fn Getpid() -> int {
    return getpid()
}
```

#### File Writing (2 functions)

Built in pure Ease using existing syscall primitives. No new compiler support needed.

```ease
fn WriteFile(path: string, data: string) -> OsResult {
    // O_WRONLY|O_CREAT|O_TRUNC = 0x0201|0x0200|0x0400 = 577 on macOS
    fd := syscall.open(path, 577, 420)  // mode 0644
    if fd < 0 { return make_error() }
    n := syscall.write(fd, data, len(data))
    syscall.close(fd)
    if n < 0 { return make_error() }
    return Ok(0)
}

fn AppendFile(path: string, data: string) -> OsResult {
    // O_WRONLY|O_CREAT|O_APPEND = 0x0201|0x0200|0x0008 = 521 on macOS
    // Actually: O_WRONLY=1, O_CREAT=0x200, O_APPEND=0x8 = 521
    fd := syscall.open(path, 521, 420)  // mode 0644
    if fd < 0 { return make_error() }
    n := syscall.write(fd, data, len(data))
    syscall.close(fd)
    if n < 0 { return make_error() }
    return Ok(0)
}
```

macOS open flags:
- `O_WRONLY` = 0x0001 (1)
- `O_CREAT` = 0x0200 (512)
- `O_TRUNC` = 0x0400 (1024)
- `O_APPEND` = 0x0008 (8)
- WriteFile: `1 + 512 + 1024 = 1537`
- AppendFile: `1 + 512 + 8 = 521`

#### Filesystem (3 functions)

```ease
extern fn remove(path: ptr) -> i32
extern fn rename(old: ptr, new: ptr) -> i32
extern fn mkdir(path: ptr, mode: i32) -> i32

fn Remove(path: string) -> OsResult {
    rc := remove(path)
    if rc != 0 { return make_error() }
    return Ok(0)
}

fn Rename(old: string, new: string) -> OsResult {
    rc := rename(old, new)
    if rc != 0 { return make_error() }
    return Ok(0)
}

fn Mkdir(path: string) -> OsResult {
    // 0755 = 493
    rc := mkdir(path, 493)
    if rc != 0 { return make_error() }
    return Ok(0)
}
```

#### File Info (2 functions)

Require LLVM helper functions (same pattern as existing `ease_is_dir`):

```ease
fn Exists(path: string) -> int      // 1 if exists, 0 if not
fn FileSize(path: string) -> int    // bytes, -1 on error
```

**`ease_file_exists`**: Call `stat()`, return 1 if it succeeds (rc == 0), 0 otherwise. No need to check specific fields.

**`ease_file_size`**: Call `stat()`, read `st_size` at offset 96 in macOS struct stat (i64). Return -1 if stat fails.

New IR opcodes:
- `OP_FILE_EXISTS` — takes path, returns 1/0
- `OP_FILE_SIZE` — takes path, returns size/-1

### Implementation Layers

| Function | Approach | Compiler changes |
|----------|----------|-----------------|
| Getenv | extern fn | None |
| Setenv | extern fn | None |
| Unsetenv | extern fn | None |
| Exit | extern fn | None |
| Getpid | extern fn | None |
| WriteFile | pure Ease (syscalls) | None |
| AppendFile | pure Ease (syscalls) | None |
| Remove | extern fn | None |
| Rename | extern fn | None |
| Mkdir | extern fn | None |
| Exists | LLVM helper + opcode | ir.ease, irgen.ease, llvm.ease |
| FileSize | LLVM helper + opcode | ir.ease, irgen.ease, llvm.ease |
| make_error | extern fn (__error, strerror) | None |

## Files Changed

- `bootstrap/ease/os/os.ease` — all 13 new functions, OsResult enum, errno helpers, extern declarations
- `bootstrap/ease/ir/ir.ease` — 2 new opcodes (OP_FILE_EXISTS, OP_FILE_SIZE)
- `bootstrap/ease/irgen/irgen.ease` — IR gen for 2 new opcodes
- `bootstrap/ease/llvm/llvm.ease` — 2 LLVM helper functions, 2 opcode emission cases, extern declarations for __error and strerror
- `tests/os_test.ease` — tests for all 13 functions
- `bootstrap/seed.ll` — bootstrap convergence
- `docs/implementation-status.md` — update os status

## Testing

Each function gets at least one test:

- `TestGetenv` — read `HOME` or `PATH`, verify non-empty
- `TestGetenvMissing` — read nonexistent var, verify ""
- `TestSetenvUnsetenv` — set a var, read it back, unset, verify ""
- `TestExit` — skip (terminates process)
- `TestGetpid` — verify > 0
- `TestWriteFile` — write to tmp file, read back, verify content
- `TestWriteFileError` — write to invalid path, verify error variant
- `TestAppendFile` — write then append, read back, verify combined content
- `TestRemove` — create file, remove it, verify Exists returns 0
- `TestRemoveNotExist` — remove nonexistent, verify NotExist variant
- `TestRename` — create file, rename, verify old gone and new exists
- `TestMkdir` — create dir, verify IsDir, then Remove
- `TestMkdirAlreadyExists` — create twice, verify AlreadyExists variant
- `TestExists` — verify existing file returns 1, nonexistent returns 0
- `TestFileSize` — write known content, check size matches

## Out of Scope

- `Environ()` — listing all env vars (requires pointer-to-pointer iteration)
- Process stdout capture (popen/pipe/fork)
- Recursive mkdir (`MkdirAll`)
- File permissions/chmod
- Symlink operations
