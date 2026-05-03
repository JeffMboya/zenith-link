CC      = gcc
CFLAGS  = -std=c99 -O2 -Wall -Wextra -Wpedantic
LDFLAGS = -lm

.PHONY: all test clean

all: test_orbitron

test_orbitron: test_orbitron.c orbitron.c orbitron.h
	$(CC) $(CFLAGS) -o $@ test_orbitron.c orbitron.c $(LDFLAGS)

test: test_orbitron
	./test_orbitron

# ARM Cortex-M4 cross-compile (requires arm-none-eabi-gcc)
cortex-m4: orbitron.c orbitron.h
	arm-none-eabi-gcc -mcpu=cortex-m4 -mthumb -mfpu=fpv4-sp-d16 -mfloat-abi=hard \
	  -Os -std=c99 -Wall -c orbitron.c -o orbitron_cortexm4.o
	@arm-none-eabi-size orbitron_cortexm4.o

clean:
	rm -f test_orbitron orbitron_cortexm4.o
