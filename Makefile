# Setup name variables for the package/tool
NAME := zh
PKG := github.com/nick96/$(NAME)

CGO_ENABLED := 0

# Set any default go build tags.
BUILDTAGS :=

include basic.mk

.PHONY: prebuild
prebuild:
