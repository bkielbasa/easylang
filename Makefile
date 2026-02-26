# Ease Programming Language - Bootstrap Build System
#
# Build the Ease compiler from its seed (no external dependencies):
#   make
#
# Verify self-hosting (compiler compiles itself identically):
#   make verify
#
# Update the seed after modifying the compiler:
#   make update-seed

CC      ?= clang
CFLAGS  := -O1
SEED    := bootstrap/seed.ll
COMPILER_SRC := bootstrap/compiler.ease

BUILD_DIR := tmp
EASE      := $(BUILD_DIR)/ease

.PHONY: all clean verify update-seed test bench

all: $(EASE)

# Build the compiler from the seed LLVM IR
$(EASE): $(SEED) | $(BUILD_DIR)
	$(CC) $(CFLAGS) $(SEED) -o $@

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

# Self-host: use the seed-built compiler to recompile itself
$(BUILD_DIR)/output.ll: $(EASE) $(COMPILER_SRC)
	$(EASE) $(COMPILER_SRC)

# Verify self-hosting convergence (seed-built compiler produces identical IR)
verify: $(EASE) | $(BUILD_DIR)
	@echo "=== Verifying self-hosting convergence ==="
	$(EASE) $(COMPILER_SRC)
	@cp $(BUILD_DIR)/output.ll $(BUILD_DIR)/verify_gen1.ll
	$(CC) $(CFLAGS) $(BUILD_DIR)/verify_gen1.ll -o $(BUILD_DIR)/ease_gen1
	$(BUILD_DIR)/ease_gen1 $(COMPILER_SRC)
	@cp $(BUILD_DIR)/output.ll $(BUILD_DIR)/verify_gen2.ll
	@diff $(BUILD_DIR)/verify_gen1.ll $(BUILD_DIR)/verify_gen2.ll \
		&& echo "=== Convergence: PASS (gen1 == gen2) ===" \
		|| (echo "=== Convergence: FAIL ===" && exit 1)

# Update the seed after making changes to the compiler source
update-seed: verify
	cp $(BUILD_DIR)/verify_gen1.ll $(SEED)
	@echo "=== Seed updated ==="

# Run tests (Go-style: fn TestXxx in *_test.ease files)
# Usage: make test [DIR=path/to/dir] (default: tests/)
DIR ?= tests
test: $(EASE)
	@$(EASE) test $(DIR)

# Run tests + benchmarks
# Usage: make bench [DIR=path/to/dir] (default: tests/)
bench: $(EASE)
	@$(EASE) test $(DIR) --bench

clean:
	rm -f $(BUILD_DIR)/ease $(BUILD_DIR)/ease_gen1
	rm -f $(BUILD_DIR)/output.ll $(BUILD_DIR)/verify_gen1.ll $(BUILD_DIR)/verify_gen2.ll
	rm -f $(BUILD_DIR)/test_bin
