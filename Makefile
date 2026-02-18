# Ease Programming Language - Bootstrap Build System
#
# Build the Ease compiler from its seed (no Go required):
#   make
#
# Verify self-hosting (compiler compiles itself identically):
#   make verify
#
# Update the seed after modifying the compiler:
#   make update-seed

CC      ?= clang
CFLAGS  := -O1
RUNTIME := runtime/ease_runtime.c
SEED    := bootstrap/seed.ll
COMPILER_SRC := bootstrap/compiler.ease

BUILD_DIR := tmp
EASE      := $(BUILD_DIR)/ease

.PHONY: all clean verify update-seed test

all: $(EASE)

# Build the compiler from the seed LLVM IR + C runtime
$(EASE): $(SEED) $(RUNTIME) | $(BUILD_DIR)
	$(CC) $(CFLAGS) $(RUNTIME) $(SEED) -o $@

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
	$(CC) $(CFLAGS) $(RUNTIME) $(BUILD_DIR)/verify_gen1.ll -o $(BUILD_DIR)/ease_gen1
	$(BUILD_DIR)/ease_gen1 $(COMPILER_SRC)
	@cp $(BUILD_DIR)/output.ll $(BUILD_DIR)/verify_gen2.ll
	@diff $(BUILD_DIR)/verify_gen1.ll $(BUILD_DIR)/verify_gen2.ll \
		&& echo "=== Convergence: PASS (gen1 == gen2) ===" \
		|| (echo "=== Convergence: FAIL ===" && exit 1)

# Update the seed after making changes to the compiler source
update-seed: verify
	cp $(BUILD_DIR)/verify_gen1.ll $(SEED)
	@echo "=== Seed updated ==="

# Run integration tests (requires the Go compiler for 'ease run')
test: $(EASE)
	@echo "================================"
	@echo "  Ease Compiler Test Suite"
	@echo "================================"
	@echo
	@passed=0; failed=0; \
	for test_file in $$(ls tests/*.ease | sort); do \
		test_name=$$(basename "$$test_file" .ease); \
		printf "Running $$test_name... "; \
		$(EASE) "$$test_file" > /dev/null 2>&1 \
			&& $(CC) $(CFLAGS) $(RUNTIME) $(BUILD_DIR)/output.ll -o $(BUILD_DIR)/test_bin 2>/dev/null \
			&& $(BUILD_DIR)/test_bin > /dev/null 2>&1; \
		if [ $$? -eq 0 ]; then \
			echo "✓ PASS"; \
			passed=$$((passed + 1)); \
		else \
			echo "✗ FAIL"; \
			failed=$$((failed + 1)); \
		fi; \
	done; \
	echo; \
	echo "================================"; \
	echo "Results: $$passed passed, $$failed failed"; \
	echo "================================"; \
	[ $$failed -eq 0 ]

clean:
	rm -f $(BUILD_DIR)/ease $(BUILD_DIR)/ease_gen1
	rm -f $(BUILD_DIR)/output.ll $(BUILD_DIR)/verify_gen1.ll $(BUILD_DIR)/verify_gen2.ll
	rm -f $(BUILD_DIR)/test_bin
