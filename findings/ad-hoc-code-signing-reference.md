# Ad-Hoc Code Signing for macOS ARM64: Implementation Reference

Research on how Go, LLVM/lld, and Zig implement ad-hoc code signing for Apple Silicon binaries, and what constitutes a minimum viable code signature.

## Table of Contents

1. [Background: Why Code Signing is Required](#background)
2. [The Minimum Viable Ad-Hoc Signature](#minimum-viable)
3. [Complete Structure Reference](#structure-reference)
4. [How Go Does It](#go-implementation)
5. [How LLVM/lld Does It](#lld-implementation)
6. [How Zig Does It](#zig-implementation)
7. [Comparison with Current Bootstrap Implementation](#bootstrap-comparison)
8. [Key Differences and Issues](#key-differences)
9. [Recommendations for Ease Compiler](#recommendations)

---

## 1. Background: Why Code Signing is Required <a name="background"></a>

Starting with macOS 11 (Big Sur) on Apple Silicon, **all native ARM64 code must be signed** or the kernel will immediately SIGKILL the process (exit code 137). This applies to both executables and dylibs.

An **ad-hoc signature** is sufficient - no Apple Developer certificate needed. The linker (`ld64`) automatically generates one. When you see `codesign -dvv` output showing `flags=0x20002(adhoc,linker-signed)`, that means the linker did the signing.

The key change from Intel to ARM64:
- **Intel (x86_64)**: Code signing optional for local executables
- **ARM64 (Apple Silicon)**: Code signing mandatory, kernel validates page hashes on load

## 2. The Minimum Viable Ad-Hoc Signature <a name="minimum-viable"></a>

Based on analysis of Go, LLVM/lld, and Zig implementations, here is the **absolute minimum** that works:

### Go's Approach (Simplest)

Go uses the simplest possible structure - a SuperBlob with exactly **one blob**: a CodeDirectory. No requirements blob, no CMS signature, no entitlements.

```
SuperBlob (12 bytes)
  BlobIndex[0] (8 bytes) -> CodeDirectory
CodeDirectory (104 bytes header)
  Identifier string (variable, null-terminated)
  Code page hashes (nCodeSlots * 32 bytes)
```

**Total blobs: 1** (just the CodeDirectory)

### What is NOT required for execution:
- Requirements blob (CSSLOT_REQUIREMENTS) - NOT required
- CMS/BlobWrapper signature (CSSLOT_SIGNATURESLOT) - NOT required
- Entitlements - NOT required
- Special hash slots - can be zero
- Team identifier - NOT required

### What IS required:
- SuperBlob container with at least 1 blob
- CodeDirectory with correct SHA-256 page hashes
- Correct `codeLimit` value
- Page size of 4096 bytes (log2 = 12)
- Proper big-endian encoding for all signature structures
- LC_CODE_SIGNATURE load command pointing to the signature

## 3. Complete Structure Reference <a name="structure-reference"></a>

### 3.1 LC_CODE_SIGNATURE Load Command

```c
// Load command in Mach-O header (LITTLE-endian, like all Mach-O)
struct linkedit_data_command {
    uint32_t cmd;       // 0x1D (LC_CODE_SIGNATURE)
    uint32_t cmdsize;   // 16
    uint32_t dataoff;   // File offset to signature in __LINKEDIT
    uint32_t datasize;  // Size of signature data
};
```

**Important**: This load command uses little-endian (like all Mach-O structures). The signature blob itself uses big-endian.

### 3.2 SuperBlob

```c
// ALL fields BIG-ENDIAN
struct CS_SuperBlob {
    uint32_t magic;     // 0xFADE0CC0 (CSMAGIC_EMBEDDED_SIGNATURE)
    uint32_t length;    // Total size of entire signature blob
    uint32_t count;     // Number of BlobIndex entries
    // CS_BlobIndex index[count] follows
};
// Size: 12 bytes + (count * 8)

struct CS_BlobIndex {
    uint32_t type;      // Slot type (e.g., CSSLOT_CODEDIRECTORY = 0)
    uint32_t offset;    // Offset from start of SuperBlob to blob data
};
// Size: 8 bytes
```

### 3.3 CodeDirectory (Version 0x20400)

This is the version used by Go and LLVM/lld for modern macOS.

```c
// ALL fields BIG-ENDIAN
struct CS_CodeDirectory {
    // Base fields (all versions)
    uint32_t magic;           // 0xFADE0C02 (CSMAGIC_CODEDIRECTORY)
    uint32_t length;          // Total blob length
    uint32_t version;         // 0x20400 (CS_SUPPORTSEXECSEG)
    uint32_t flags;           // 0x20002 (CS_ADHOC | CS_LINKER_SIGNED)
    uint32_t hashOffset;      // Offset from start of CD to first code hash
    uint32_t identOffset;     // Offset from start of CD to identifier string
    uint32_t nSpecialSlots;   // Number of special hash slots (can be 0)
    uint32_t nCodeSlots;      // Number of code page hashes
    uint32_t codeLimit;       // Byte count of code covered by hashes
    uint8_t  hashSize;        // 32 (SHA-256 digest length)
    uint8_t  hashType;        // 2 (CS_HASHTYPE_SHA256)
    uint8_t  platform;        // 0 (not platform binary)
    uint8_t  pageSize;        // 12 (log2(4096))
    uint32_t spare2;          // 0 (unused)

    // Version 0x20100+
    uint32_t scatterOffset;   // 0 (no scatter vector)

    // Version 0x20200+
    uint32_t teamOffset;      // 0 (no team ID)

    // Version 0x20300+
    uint32_t spare3;          // 0 (unused)
    uint64_t codeLimit64;     // 0 (use 32-bit codeLimit for < 4GB)

    // Version 0x20400+
    uint64_t execSegBase;     // File offset of __TEXT segment (typically 0)
    uint64_t execSegLimit;    // Size of __TEXT segment
    uint64_t execSegFlags;    // CS_EXECSEG_MAIN_BINARY (0x1) for executables

    // Total fixed header: 104 bytes

    // Variable data follows:
    // [nSpecialSlots * hashSize bytes] - special hashes (BEFORE hashOffset!)
    // [nCodeSlots * hashSize bytes]    - code page hashes (AT hashOffset)
    // [identifier string]              - null-terminated C string (AT identOffset)
};
```

**CodeDirectory fixed header size**: 104 bytes (13 uint32 + 4 uint8 + 4 uint64)

### 3.4 Hash Slot Layout

The hash slots are stored like this, relative to the CodeDirectory start:

```
CD start + hashOffset - (nSpecialSlots * hashSize) : special slot [-nSpecialSlots]
...
CD start + hashOffset - hashSize                   : special slot [-1]
CD start + hashOffset                              : code slot [0]
CD start + hashOffset + hashSize                   : code slot [1]
...
CD start + hashOffset + (nCodeSlots-1) * hashSize  : code slot [nCodeSlots-1]
```

Special slots use negative indices and are stored BEFORE hashOffset. The field `hashOffset` points to code slot [0].

### 3.5 Special Hash Slot Indices

```
Slot -1 (CSSLOT_INFOSLOT):        Info.plist hash
Slot -2 (CSSLOT_REQUIREMENTS):    Requirements blob hash
Slot -3 (CSSLOT_RESOURCEDIR):     CodeResources hash
Slot -4 (CSSLOT_APPLICATION):     Application-specific (unused)
Slot -5 (CSSLOT_ENTITLEMENTS):    Entitlements (XML) hash
Slot -6:                           (reserved)
Slot -7:                           DER entitlements hash
```

When `nSpecialSlots = 0`, no special slots exist and `hashOffset` points directly to code slot [0].

### 3.6 Page Hashing Algorithm

```
page_size = 4096 (0x1000)
nCodeSlots = ceil(codeLimit / page_size)

for i in 0..nCodeSlots:
    offset = i * page_size
    size = min(page_size, codeLimit - offset)
    hash[i] = SHA256(binary_data[offset .. offset+size])
```

**Critical**: `codeLimit` defines how much of the file is covered by code hashes. This is typically everything up to (but not including) the code signature itself. All compilers set `codeLimit` to the file offset of the code signature blob.

### 3.7 Requirements Blob (Optional)

```c
// Empty requirements blob (12 bytes)
struct CS_Requirements {
    uint32_t magic;   // 0xFADE0C01 (CSMAGIC_REQUIREMENTS)
    uint32_t length;  // 12
    uint32_t count;   // 0 (no requirements)
};
```

### 3.8 Magic Numbers Reference

```
CSMAGIC_REQUIREMENT           = 0xFADE0C00  // Single requirement
CSMAGIC_REQUIREMENTS          = 0xFADE0C01  // Requirements vector (super blob)
CSMAGIC_CODEDIRECTORY         = 0xFADE0C02  // CodeDirectory
CSMAGIC_EMBEDDED_SIGNATURE    = 0xFADE0CC0  // Embedded signature (SuperBlob)
CSMAGIC_DETACHED_SIGNATURE    = 0xFADE0CC1  // Detached signature
CSMAGIC_BLOBWRAPPER           = 0xFADE0B01  // CMS signature wrapper
CSMAGIC_EMBEDDED_ENTITLEMENTS = 0xFADE7171  // Entitlements (XML)
CSMAGIC_EMBEDDED_DER_ENTITLEMENTS = 0xFADE7172  // Entitlements (DER)
```

### 3.9 Flag Values Reference

```
CS_ADHOC          = 0x00000002  // Ad-hoc signed (no certificate)
CS_LINKER_SIGNED  = 0x00020000  // Signed by linker automatically
CS_HARD           = 0x00000100  // Don't load invalid pages
CS_KILL           = 0x00000200  // Kill process if invalid
CS_RUNTIME        = 0x00010000  // Hardened runtime

// Combined: adhoc + linker-signed
CS_ADHOC | CS_LINKER_SIGNED = 0x00020002 = 0x20002
```

### 3.10 Version Numbers

```
CS_SUPPORTSSCATTER   = 0x20100  // Adds scatterOffset
CS_SUPPORTSTEAMID    = 0x20200  // Adds teamOffset
CS_SUPPORTSCODELIMIT64 = 0x20300  // Adds codeLimit64
CS_SUPPORTSEXECSEG   = 0x20400  // Adds execSeg* fields (MOST COMMON)
CS_SUPPORTSRUNTIME   = 0x20500  // Adds runtime + preEncryptOffset
CS_SUPPORTSLINKAGE   = 0x20600  // Adds linkage fields
```

## 4. How Go Does It <a name="go-implementation"></a>

Source: `cmd/internal/codesign/codesign.go`

### Go's Approach

Go uses the **simplest possible approach**: a single CodeDirectory blob in the SuperBlob. No requirements blob, no CMS signature.

### Key Design Decisions

1. **SuperBlob count = 1**: Only a CodeDirectory, nothing else
2. **Version = 0x20400**: Supports executable segment info
3. **Flags = 0x20002**: `CS_ADHOC | CS_LINKER_SIGNED`
4. **nSpecialSlots = 0**: No special hash slots at all
5. **Hash type = SHA-256** (32 bytes per hash)
6. **Page size = 4096** (pageSize field = 12, meaning 2^12)
7. **Identifier**: Uses the binary filename

### Layout Calculation

```go
// Go's Size() function
func Size(codeSize int64, id string) int64 {
    nhashes := (codeSize + pageSize - 1) / pageSize
    idOff := int64(codeDirectorySize)              // 104
    hashOff := idOff + int64(len(id) + 1)          // after identifier + null
    cdirSz := hashOff + nhashes * 32               // + all hashes
    return int64(superBlobSize + blobSize) + cdirSz // 12 + 8 + cdirSz
}
```

### Go's Layout (in order):

```
Offset 0:    SuperBlob header (12 bytes)
Offset 12:   BlobIndex[0] - type=0, offset=20 (8 bytes)
Offset 20:   CodeDirectory header (104 bytes)
Offset 124:  Identifier string + null terminator
Offset 124+len(id)+1: Code page hashes (nhashes * 32 bytes)
```

**Key insight**: Go places the identifier BEFORE the hashes. The `identOffset` points into the CodeDirectory at offset 104 (right after the header), and `hashOffset` points after the identifier string.

### Go's Sign() Function Flow

1. Calculate number of pages: `(codeSize + 4095) / 4096`
2. Calculate `identOffset = 104` (right after CD header)
3. Calculate `hashOffset = 104 + len(id) + 1`
4. Write SuperBlob header
5. Write single BlobIndex entry
6. Write CodeDirectory header
7. Write identifier string with null terminator
8. Read binary data in 4096-byte chunks, SHA-256 hash each, write hash

### execSeg Fields

Go populates the executable segment fields:
- `execSegBase`: File offset of `__TEXT` segment
- `execSegLimit`: Size of `__TEXT` segment
- `execSegFlags`: `CS_EXECSEG_MAIN_BINARY` (0x1) for main executables

## 5. How LLVM/lld Does It <a name="lld-implementation"></a>

Source: `lld/MachO/SyntheticSections.cpp`

### lld's Approach

lld also uses a **single CodeDirectory blob** in the SuperBlob, same as Go. No requirements, no CMS signature.

### Key Design Decisions

1. **SuperBlob count = 1**: Only CodeDirectory
2. **Version = CS_SUPPORTSEXECSEG** (0x20400)
3. **Flags = CS_ADHOC | CS_LINKER_SIGNED** (0x20002)
4. **nSpecialSlots = 0**: No special hash slots
5. **Hash type = SHA-256** (kSecCodeSignatureHashSHA256)
6. **Block size = 4096** (blockSizeShift = 12)
7. **Identifier**: Uses output filename (install_name or -o name)
8. **Alignment**: 16-byte alignment for the entire signature section (required by libstuff)
9. **Parallel hashing**: Uses `parallelFor` for performance on large binaries

### lld Layout

```
Offset 0:     SuperBlob header (12 bytes)
Offset 12:    BlobIndex[0] (8 bytes)
Offset 20:    CodeDirectory header (fixed size)
              Identifier string + padding to 16-byte alignment
              Code page hashes
```

lld aligns `allHeadersSize` (which includes CD header + identifier) to 16 bytes:
```cpp
allHeadersSize = alignTo<16>(fixedHeadersSize + fileName.size() + 1);
```

### Platform-specific: msync()

On macOS, after writing hashes, lld calls:
```cpp
msync(buf, fileOff + getSize(), MS_INVALIDATE);
```
This invalidates the kernel's cached signature, forcing it to re-read the new one. This is important when overwriting a previously signed binary.

## 6. How Zig Does It <a name="zig-implementation"></a>

Source: `src/link/MachO/CodeSignature.zig`

### Zig's Approach

Zig uses the **most complete approach** of the three, with support for multiple blobs:

1. **CodeDirectory** (always)
2. **Requirements** (optional)
3. **Entitlements** (optional, loaded from file)
4. **Signature/BlobWrapper** (optional, minimal stub)

### Key Design Decisions

1. **SuperBlob count = variable** (1-4 blobs)
2. **Version = CS_SUPPORTSEXECSEG** (0x20400)
3. **Flags = CS_ADHOC | CS_LINKER_SIGNED**
4. **nSpecialSlots = 7** (maximum, even if unused): Zig reserves all 7 special slots
5. **Special slot hashing**: When optional blobs are present, their content is hashed and stored in the corresponding special slot
6. **Parallel hashing**: Uses Zig's standard hasher for page hashing

### Zig's Special Slot Handling

Zig reserves 7 special slots always, but only populates them when the corresponding blob is present:
- Slot 2: Hash of requirements blob (if present)
- Slot 5: Hash of entitlements blob (if present)
- Other slots: All zeros

### Why Zig is More Complex

Zig supports **hot-code reloading** on macOS ARM64, which requires entitlements. The entitlements blob (`com.apple.security.get-task-allow`) must be embedded in the code signature for the debugger to attach.

## 7. Comparison with Current Bootstrap Implementation <a name="bootstrap-comparison"></a>

Looking at `bootstrap/compiler.ease` lines 4833-4937, the current implementation:

### What it does correctly:
- SuperBlob magic (0xFADE0CC0) - correct
- CodeDirectory magic (0xFADE0C02) - correct
- Version 0x20400 - correct (matches Go and lld)
- Flags 0x20002 (adhoc | linker-signed) - correct
- SHA-256 hash type - correct
- Page size 12 (log2 4096) - correct
- Big-endian for signature structures - correct
- Separate sig_buf and bin_buf parameters - correct

### Potential Issues:

1. **Only 1 code hash slot (`nCodeSlots = 1`)**:
   - The bootstrap compiler sets `nCodeSlots = 1` and computes a single SHA-256 hash over the entire `codeLimit`
   - Go and lld compute **per-page hashes** (one hash per 4096-byte page)
   - For a 32KB binary: should be 8 hash slots, not 1
   - The kernel validates per-page, so a single hash of the whole file may fail validation
   - **This is likely the root cause of the SIGKILL on macOS 15.x**

2. **Includes Requirements blob (unnecessary but not harmful)**:
   - Go's approach works with just 1 blob (CodeDirectory only)
   - Having an empty requirements blob shouldn't cause issues
   - But the requirements blob's count field is written as little-endian (`write_u32_le`) - should be big-endian

3. **hashOffset and identOffset layout**:
   - hashOffset = 52: This means hashes start at CD_start + 52, which is **inside the CD header** (header is 104 bytes for v0x20400)
   - Go sets hashOffset = 104 + len(id) + 1 (after header + identifier)
   - This is a **critical bug** - hashes are overwriting CD header fields

4. **identOffset = 88**:
   - At offset 88 from CD start, this lands on the `execSegBase` field (offset 72+8+8 = 88)
   - The identifier overwrites the execSegFlags field
   - Go sets identOffset = 104 (right after the full header)

5. **codeLimit64, execSegBase, execSegLimit, execSegFlags written as little-endian**:
   - Uses `write_u64_le` for these fields
   - All CodeDirectory fields must be **big-endian**
   - Go uses `put64be` for all uint64 fields

6. **Fixed codesig_size = 256**:
   - Pre-allocated size doesn't account for per-page hashing
   - For a 96KB binary: need ceil(96KB / 4KB) = 24 hash slots = 24 * 32 = 768 bytes of hashes alone
   - 256 bytes is far too small

### Summary of Bugs in Bootstrap

| Issue | Impact | Fix Difficulty |
|-------|--------|---------------|
| Single hash instead of per-page | SIGKILL | Medium (need loop) |
| hashOffset inside header | Corrupted CD | Easy (set to 104+) |
| identOffset inside header | Corrupted CD | Easy (set to 104) |
| uint64 fields as little-endian | Invalid CD | Easy (use write_u32_be) |
| Requirements count as LE | Minor | Easy |
| Fixed 256-byte allocation | Overflow | Medium (dynamic calc) |

## 8. Key Differences and Issues <a name="key-differences"></a>

### What All Three Compilers Agree On

| Field | Go | lld | Zig |
|-------|-----|-----|-----|
| SuperBlob magic | 0xFADE0CC0 | 0xFADE0CC0 | 0xFADE0CC0 |
| CD version | 0x20400 | 0x20400 | 0x20400 |
| CD flags | 0x20002 | 0x20002 | 0x20002 |
| Hash type | SHA-256 (2) | SHA-256 (2) | SHA-256 (2) |
| Hash size | 32 | 32 | 32 |
| Page size | 12 (4096) | 12 (4096) | 12 (4096) |
| Per-page hashing | Yes | Yes | Yes |
| Big-endian CD | Yes | Yes | Yes |

### Where They Differ

| Feature | Go | lld | Zig |
|---------|-----|-----|-----|
| Blob count | 1 | 1 | 1-4 |
| nSpecialSlots | 0 | 0 | 7 |
| Requirements blob | No | No | Optional |
| Entitlements | No | No | Optional |
| CMS blob | No | No | Optional (stub) |
| execSeg fields | Populated | Populated | Populated |
| Identifier | Filename | Filename | Filename |
| Alignment | None | 16-byte | Varies |
| msync() | No | Yes (macOS) | No |

### Go is the Simplest Reference

Go proves that the **absolute minimum** is:
- 1 blob (CodeDirectory only)
- 0 special slots
- Per-page SHA-256 hashes
- Correct `codeLimit`
- No requirements, no CMS

## 9. Recommendations for Ease Compiler <a name="recommendations"></a>

### Immediate Fix (to get binaries running without `codesign -s -`)

Follow Go's approach exactly:

1. **Calculate proper sizes**:
```
nCodeSlots = ceil(codeLimit / 4096)
identOffset = 104                        // right after CD header
hashOffset = 104 + len(identifier) + 1   // after identifier
cdSize = hashOffset + (nCodeSlots * 32)
totalSize = 12 + 8 + cdSize              // SuperBlob(12) + BlobIndex(8) + CD
```

2. **Use 1 blob, 0 special slots**:
```
SuperBlob:
  magic = 0xFADE0CC0 (big-endian)
  length = totalSize (big-endian)
  count = 1 (big-endian)

BlobIndex[0]:
  type = 0 (CSSLOT_CODEDIRECTORY, big-endian)
  offset = 20 (12 + 8, big-endian)
```

3. **Write CodeDirectory header with ALL fields big-endian**:
```
magic         = 0xFADE0C02
length        = cdSize
version       = 0x20400
flags         = 0x20002
hashOffset    = (calculated above)
identOffset   = 104
nSpecialSlots = 0
nCodeSlots    = (calculated above)
codeLimit     = (file offset of signature)
hashSize      = 32
hashType      = 2
platform      = 0
pageSize      = 12
spare2        = 0
scatterOffset = 0
teamOffset    = 0
spare3        = 0
codeLimit64   = 0  (BIG-ENDIAN!)
execSegBase   = 0  (BIG-ENDIAN!)  // or __TEXT fileoff
execSegLimit  = textSegSize (BIG-ENDIAN!)
execSegFlags  = 1  (BIG-ENDIAN!)  // CS_EXECSEG_MAIN_BINARY
```

4. **Write identifier**: null-terminated string right after header

5. **Compute per-page hashes**:
```
for page = 0 to nCodeSlots-1:
    start = page * 4096
    end = min(start + 4096, codeLimit)
    hash = SHA256(binary[start..end])
    write hash at (cdStart + hashOffset + page * 32)
```

6. **Pre-calculate total signature size** before writing the binary:
```
sigSize = 12 + 8 + 104 + len(id) + 1 + (nCodeSlots * 32)
sigSize = alignUp(sigSize, 16)  // 16-byte alignment
```

### codeLimit Must Equal Signature Offset

`codeLimit` tells the kernel "hash everything from byte 0 to byte codeLimit-1". This must be the file offset where the code signature blob starts. Everything before the signature is hashed, the signature itself is not included in the hashes.

### Byte Order Summary

- **Mach-O header, load commands, segment commands**: Little-endian
- **LC_CODE_SIGNATURE load command**: Little-endian
- **SuperBlob, CodeDirectory, all signature blobs**: Big-endian
- **All uint64 fields in CodeDirectory**: Big-endian

### Alignment Requirements

- Code signature data offset should be 16-byte aligned in the file
- The `__LINKEDIT` segment's file size must be updated to include the signature
- The `__LINKEDIT` VM size should be page-aligned (typically 16KB on ARM64)

---

## Sources

- [Go codesign package source](https://github.com/golang/go/blob/master/src/cmd/internal/codesign/codesign.go)
- [Go macho linker](https://github.com/golang/go/blob/master/src/cmd/link/internal/ld/macho.go)
- [Go codesign API docs](https://pkg.go.dev/cmd/internal/codesign)
- [LLVM lld MachO SyntheticSections](https://github.com/llvm/llvm-project/blob/main/lld/MachO/SyntheticSections.cpp)
- [Zig MachO CodeSignature](https://github.com/ziglang/zig/blob/master/src/link/MachO/CodeSignature.zig)
- [Zig linker code signing issue](https://github.com/ziglang/zig/issues/7103)
- [Apple XNU cs_blobs.h](https://github.com/apple-oss-distributions/xnu/blob/main/osfmk/kern/cs_blobs.h)
- [Apple ld64 cs_blobs.h](https://github.com/apple-opensource/ld64/blob/master/src/ld/cs_blobs.h)
- [Apple dyld CodeSigningTypes.h](https://github.com/apple-oss-distributions/dyld/blob/main/common/CodeSigningTypes.h)
- [In-depth look at ad-hoc signing - Alfie CG](https://alfiecg.uk/2024/01/06/Ad-hoc-signing.html)
- [Reproducible codesigning on Apple Silicon - Keith Smiley](https://www.smileykeith.com/2021/10/05/codesign-m1/)
- [LC_CODE_SIGNATURE documentation](https://github.com/qyang-nj/llios/blob/main/macho_parser/docs/LC_CODE_SIGNATURE.md)
- [macOS Code Signing - HackTricks](https://book.hacktricks.wiki/en/macos-hardening/macos-security-and-privilege-escalation/macos-security-protections/macos-code-signing.html)
- [Apple TN3126: Inside Code Signing: Hashes](https://developer.apple.com/documentation/technotes/tn3126-inside-code-signing-hashes)
- [Apple TN3127: Inside Code Signing: Requirements](https://developer.apple.com/documentation/technotes/tn3127-inside-code-signing-requirements)
- [Go issue #42684: macOS on arm64 requires codesigning](https://github.com/golang/go/issues/42684)
- [FOSDEM 2021: Mach-O linker in Zig](https://archive.fosdem.org/2021/schedule/event/zig_macho/)
- [Zig hot-code reloading entitlements](https://www.jakubkonka.com/2022/03/22/hcs-zig-part-two.html)
- [.NET macOS ad-hoc signing issue](https://github.com/dotnet/sdk/issues/34917)
- [Homebrew macOS 11 codesigning issue](https://github.com/Homebrew/brew/issues/9082)
- [Apple Silicon code signing requirement](https://eclecticlight.co/2020/08/22/apple-silicon-macs-will-require-signed-code/)
