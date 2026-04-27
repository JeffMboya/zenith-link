CC      = gcc
CFLAGS  = -std=c99 -O2 -Wall -Wextra -Wpedantic
LDFLAGS = -lm

.PHONY: all test clean

all: test_zenith_link

test_zenith_link: test_zenith_link.c zenith_link.c zenith_link.h
	$(CC) $(CFLAGS) -o $@ test_zenith_link.c zenith_link.c $(LDFLAGS)

test: test_zenith_link
	./test_zenith_link

# ARM Cortex-M4 cross-compile (requires arm-none-eabi-gcc)
cortex-m4: zenith_link.c zenith_link.h
	arm-none-eabi-gcc -mcpu=cortex-m4 -mthumb -mfpu=fpv4-sp-d16 -mfloat-abi=hard \
	  -Os -std=c99 -Wall -c zenith_link.c -o zenith_link_cortexm4.o
	@arm-none-eabi-size zenith_link_cortexm4.o

clean:
	rm -f test_zenith_link zenith_link_cortexm4.o
