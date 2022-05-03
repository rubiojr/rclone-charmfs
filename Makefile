all: build

build:
	./script/build

test: clean
	./script/test

clean:
	rm -rf tmp
	rm -rf tools

.PHONY: build clean test

