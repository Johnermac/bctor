# Root Makefile

SECCOMP_DIR = lib/seccomp
BINARY = bctor

.PHONY: all seccomplib gobuild run clean

all: seccomplib gobuild

seccomplib:
	$(MAKE) -C $(SECCOMP_DIR)

gobuild:
	CGO_LDFLAGS="-L$(PWD)/$(SECCOMP_DIR) -lseccompfilter" \
	CGO_CFLAGS="-I$(PWD)/$(SECCOMP_DIR)" \
	go build -o $(BINARY) .

run: all
	./$(BINARY)

clean:
	$(MAKE) -C $(SECCOMP_DIR) clean
	rm -f $(BINARY) 