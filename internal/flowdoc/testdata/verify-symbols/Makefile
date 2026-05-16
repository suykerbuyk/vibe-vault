# Makefile fixture for verify symbol-grammar tests.

CC := gcc
CFLAGS := -O2

build: src/main.o
	$(CC) -o app $<

clean:
	rm -f app

.PHONY: build clean
