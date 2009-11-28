#not an actual package yet: just main functions. This file produces an executable, main
include $(GOROOT)/src/Make.$(GOARCH)

TARG=go-bot
GOFILES=\
        main.go\
        irc.go\
        run.go\

#include $(GOROOT)/src/Make.pkg

main: main.${O}
	${LD} -o main main.${O}

main.${O}: ${GOFILES}
	${GC} -o main.${O} ${GOFILES}

