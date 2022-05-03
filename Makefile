all: build

build:
	./script/build

test: clean
	./script/test

clean:
	rm -rf tmp

.PHONY: build clean test

