# lib/seccomp/Makefile

CC = gcc
CFLAGS = -Wall -Wextra -O2 -fPIC

.PHONY: all clean

all: libseccompfilter.a

libseccompfilter.a: filter.o install.o
	ar rcs $@ $^

filter.o: filter.c rules.h filter.h
	$(CC) $(CFLAGS) -c filter.c -o filter.o

install.o: install.c install.h filter.h
	$(CC) $(CFLAGS) -c install.c -o install.o

clean:
	rm -f *.o *.a
