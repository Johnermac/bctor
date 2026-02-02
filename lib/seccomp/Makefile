# lib/seccomp/Makefile

all: libseccompfilter.a

libseccompfilter.a: filter.o install.o
    ar rcs $@ $^

filter.o: filter.c rules.h
    gcc -c filter.c -o filter.o

install.o: install.c install.h rules.h
    gcc -c install.c -o install.o

clean:
    rm -f *.o libseccompfilter.a