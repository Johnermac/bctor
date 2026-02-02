# Root Makefile

SECCOMP_DIR = lib/seccomp
BINARY = bctor

all: seccomplib gobuild

seccomplib:
    $(MAKE) -C $(SECCOMP_DIR)

gobuild:
    go build -o $(BINARY) ./...

run: all
    ./$(BINARY)

clean:
    $(MAKE) -C $(SECCOMP_DIR) clean
    rm -f $(BINARY)