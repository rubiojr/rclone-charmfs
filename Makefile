all: build

build:
	./script/build

test: clean
	./script/test

clean:
	rm -rf tmp

nuke: clean
	rm -rf tools vendor rclone.conf rclone-charmfs

.PHONY: build clean test nuke
