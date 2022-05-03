all: build

build:
	./script/build

test: clean
	./script/test

clean:
	rm -rf tmp

nuke: clean
	rm -rf tools rclone rclone.conf rclone-charmfs

lint:
	golangci-lint run

.PHONY: build clean test nuke lint
