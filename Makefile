all: build

build:
	./script/build

test: clean
	./script/test

clean:
	rm -rf tmp

nuke: clean
	rm -rf tools vendor

.PHONY: build clean test nuke
