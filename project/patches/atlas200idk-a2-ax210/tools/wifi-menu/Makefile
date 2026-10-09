CC ?= cc
CFLAGS ?= -O3 -flto -fomit-frame-pointer -fno-ident -DNDEBUG -std=c11 -Wall -Wextra -Werror
LDFLAGS ?= -flto -Wl,-O1,--as-needed -lutil
PREFIX ?= /usr/local

all: wifi-menu

wifi-menu: wifi-menu.c
	$(CC) $(CFLAGS) $(LDFLAGS) -o $@ $<

install: wifi-menu
	install -D -m 0755 wifi-menu $(DESTDIR)$(PREFIX)/bin/wifi-menu

clean:
	rm -f wifi-menu

.PHONY: all install clean
