# macOS Code Signing and Executable Security

## The Problem

On modern macOS (especially 15.x+), simply creating a Mach-O binary with correct structure isn't enough - the OS performs security checks that can silently kill your program.

## What Happens Without Proper Signing

### Symptom
```bash
$ ./my_program
(process exits with code 137 - SIGKILL)
```

No error message, no explanation - just **instant death** via SIGKILL from the kernel.

### Why This Happens

macOS security features:
1. **Gatekeeper** - Verifies app signatures
2. **XProtect** - Malware scanning
3. **Code signing enforcement** - Validates code hasn't been tampered with
4. **Hardened runtime** - Additional security restrictions

Even for local development, macOS 15.x enforces stricter rules than previous versions.

## Code Signature Structure

A code signature is embedded in the `__LINKEDIT` segment of the Mach-O binary:

```
Binary Layout:
├─ Mach-O Header
├─ Load Commands
│  ├─ LC_SEGMENT_64 (__TEXT)    ; Code
│  ├─ LC_SEGMENT_64 (__DATA)    ; Data
│  ├─ LC_SEGMENT_64 (__LINKEDIT); Signatures, symbols
│  └─ LC_CODE_SIGNATURE         ; Points to signature blob
├─ __TEXT segment (code)
├─ __DATA segment (data)
└─ __LINKEDIT segment
   ├─ Symbol table
   ├─ String table
   └─ Code Signature Blob ← Here!
```

## Code Signature Blob Structure

The signature is a nested structure of "blobs":

```
SuperBlob (type: CSMAGIC_EMBEDDED_SIGNATURE)
├─ CodeDirectory (type: CSMAGIC_CODEDIRECTORY)
│  ├─ Version
│  ├─ Flags
│  ├─ Hash offset
│  ├─ Identifier offset
│  ├─ Special hashes[-5 to -1]
│  ├─ Code hash[0..n]       ← SHA-256 hashes of code pages
│  └─ Identifier string     ← "com.example.myprogram"
└─ Requirements (optional)
```

### SuperBlob Structure
```c
struct CS_SuperBlob {
    uint32_t magic;         // 0xFADE0CC0
    uint32_t length;        // Total size
    uint32_t count;         // Number of sub-blobs
    struct {
        uint32_t type;      // CSSLOT_CODEDIRECTORY
        uint32_t offset;    // Offset to CodeDirectory
    } index[count];
};
```

### CodeDirectory Structure
```c
struct CS_CodeDirectory {
    uint32_t magic;         // 0xFADE0C02
    uint32_t length;        // Size of this blob
    uint32_t version;       // Format version (0x20500 for SHA-256)
    uint32_t flags;         // Various flags
    uint32_t hashOffset;    // Offset to hash array
    uint32_t identOffset;   // Offset to identifier string
    uint32_t nSpecialSlots; // Number of special hashes
    uint32_t nCodeSlots;    // Number of code page hashes
    uint32_t codeLimit;     // Size of code to hash
    uint8_t  hashSize;      // 32 for SHA-256
    uint8_t  hashType;      // 2 for SHA-256
    // ... more fields ...
    // Followed by:
    // - Special hashes (nSpecialSlots * hashSize bytes)
    // - Code hashes (nCodeSlots * hashSize bytes)
    // - Identifier string (null-terminated)
};
```

## Computing the Code Hashes

The OS verifies that code hasn't been modified by checking SHA-256 hashes:

### Algorithm
```c
void compute_code_signature(uint8_t* code, size_t code_size) {
    const size_t PAGE_SIZE = 4096;

    // 1. Calculate number of pages (round up)
    size_t nCodeSlots = (code_size + PAGE_SIZE - 1) / PAGE_SIZE;

    // 2. Hash each 4KB page
    for (size_t i = 0; i < nCodeSlots; i++) {
        size_t offset = i * PAGE_SIZE;
        size_t chunk_size = min(PAGE_SIZE, code_size - offset);

        uint8_t hash[32];
        sha256(code + offset, chunk_size, hash);

        // Store hash in CodeDirectory
        memcpy(hashes + i * 32, hash, 32);
    }
}
```

### Example

For a 12KB code section:
```
Page 0: [0x0000 - 0x0FFF] → SHA-256 hash → slot 0
Page 1: [0x1000 - 0x1FFF] → SHA-256 hash → slot 1
Page 2: [0x2000 - 0x2FFF] → SHA-256 hash → slot 2
```

## Identifier String

The identifier is a reverse-DNS style string:

```
"com.example.myprogram"
"org.opensource.mylanguage"
```

This identifies your executable in the system logs and security frameworks.

## Minimal Working Signature

For local development, you can create a minimal signature:

```c
// 1. SuperBlob header
write_u32_be(CSMAGIC_EMBEDDED_SIGNATURE);  // 0xFADE0CC0
write_u32_be(total_size);
write_u32_be(1);  // 1 sub-blob (CodeDirectory)
write_u32_be(CSSLOT_CODEDIRECTORY);  // 0
write_u32_be(12);  // Offset to CodeDirectory (after SuperBlob header)

// 2. CodeDirectory
write_u32_be(CSMAGIC_CODEDIRECTORY);  // 0xFADE0C02
write_u32_be(cd_size);
write_u32_be(0x20500);  // Version (SHA-256 support)
write_u32_be(0);  // Flags
write_u32_be(hashOffset);  // Where hashes start
write_u32_be(identOffset);  // Where identifier starts
write_u32_be(5);  // nSpecialSlots
write_u32_be(nCodeSlots);  // Number of 4KB pages
write_u32_be(codeLimit);  // Size of code
write_u8(32);  // SHA-256 hash size
write_u8(2);   // SHA-256 hash type
// ... (fill other fields)

// 3. Special hashes (all zeros for minimal signature)
for (int i = 0; i < 5; i++) {
    write_zeros(32);  // Empty special slots
}

// 4. Code page hashes
for (int i = 0; i < nCodeSlots; i++) {
    uint8_t hash[32];
    size_t offset = i * 4096;
    size_t size = min(4096, codeLimit - offset);
    sha256(code_base + offset, size, hash);
    write_bytes(hash, 32);
}

// 5. Identifier
write_string("com.example.myprogram");
write_u8(0);  // Null terminator
```

## Common Pitfalls

### ❌ All-Zero Hashes
```c
// This doesn't work!
memset(hashes, 0, nCodeSlots * 32);
```
macOS **validates the hashes**. Zero hashes mean "no code" and will fail verification.

### ❌ Wrong Offsets
```c
// Writing hash at wrong location
size_t offset = 0;  // Should be: cd_start + hashOffset
sha256(code, size, signature + offset);
```
The hash must be written at **CodeDirectory start + hashOffset**, not sequentially.

### ❌ Wrong Code Limit
```c
// Hashing more than exists
codeLimit = 100000;  // But only have 10000 bytes of code
```
The `codeLimit` must exactly match the size of the __TEXT segment code.

### ✅ Correct Implementation
```c
// Separate buffers for binary and signature
uint8_t* binary_buffer = ...; // Contains actual code
uint8_t* sig_buffer = ...;    // Separate signature buffer

// Jump to correct offset in signature buffer
size_t hash_offset = cd_start + hashOffset;
sha256(binary_buffer, codeLimit, sig_buffer + hash_offset);
```

## Testing Your Signature

### View signature info
```bash
codesign -dvv ./myprogram
```

### Verify signature
```bash
codesign -v ./myprogram
echo $?  # 0 = valid
```

### Extract signature
```bash
codesign -d --extract-certificates ./myprogram
```

## Running Without Full Signing

Even with a minimal signature, macOS 15.x may still block execution. Workarounds:

### 1. Run in lldb
```bash
lldb ./myprogram -o "process launch" -o "quit"
```
Debuggers have special permissions and can run unsigned code.

### 2. Ad-hoc signing
```bash
codesign -s - ./myprogram
```
Creates a local signature (not trusted by Gatekeeper but better than nothing).

### 3. Disable security (development only!)
```bash
# Disable System Integrity Protection (requires reboot)
csrutil disable  # In Recovery Mode
```
⚠️ **Not recommended** - leaves your system vulnerable.

## Why This Matters for Compilers

If you're writing a compiler that generates native executables:

1. **Must generate valid Mach-O** structure
2. **Must include LC_CODE_SIGNATURE** load command
3. **Must embed actual signature blob** in __LINKEDIT
4. **Must compute correct SHA-256 hashes** of code pages
5. **Must handle endianness** (big-endian for signature structures)

Without this, your generated programs won't run on modern macOS, even in development.

## Resources

- [Apple Code Signing Guide](https://developer.apple.com/library/archive/documentation/Security/Conceptual/CodeSigningGuide/)
- `/usr/include/mach-o/loader.h` - Load command definitions
- `man codesign` - Code signing tool
- [XNU source code](https://opensource.apple.com/source/xnu/) - Kernel implementation

## Key Takeaways

1. **Code signing is mandatory** on modern macOS
2. **SHA-256 hashes** verify code integrity
3. **4KB page granularity** for hashing
4. **Structure is complex** but can start minimal
5. **Use separate buffers** for binary and signature data
6. **Offsets are critical** - must calculate correctly
7. **lldb provides workaround** during development
8. **Ad-hoc signing** (`codesign -s -`) for local testing
