# Rclone :heart: CharmFS

A [rclone](https://github.com/rclone/rclone) CharmFS backend to manage your [Charm Cloud](https://charm.sh) files.

The backend is currently highly experimental, in a working prototype stage, very inefficient and with some functionality broken.

**Don't use it with real/production data for now.**

## Building

Install [Go](https://go.dev) and Run `make`.

This will clone the rclone repository, add the CharmFS backend code and build the `rclone-charm` binary.

## Running

Configure the remote firt:

```
rclone-charm config create charmfs charm url=https://cloud.charm.sh
```

If you want to use your own Charm server URL, it needs to be set using environment variables for now, as remote rclone config is currently ignored (not implemented):

```
export CHARM_HOST=my.charm.host

rclone-charm ls charmfs:
```

## Working commands

Far from being bug free and efficient, may eat data.

* cat
* copy
* delete
* deletefile
* ls
* mount (partially, VFS caching doesn't work as expected, which means opening files with other programs will be broken)
* tree

## Unsupported commands

* about (No quota support in CharmFS to my knowledge) 

## Commands not working

* mkdir (not sure yet if we can mkdir empty directories in Charm)

## Commands not tested

Everything else.

## Planed

* Custom config endpoint support (supported via CHARM_* env vars currently)
* Unit tests
* Replace Bash integration tests with Go test
* Upstream submission

## Gotchas

CharmFS uses end-to-end encryption, which makes some Rclone file operations a bit more complicated or downright impossible. In particular, things like:

* Getting the remote (plaintext) file size without downloading it first
* Get the remote real file name
* Checksumming local and remote files,
* Comparing local and remote file sizes

and other file operations where plaintext access by the server is required will probably be never supported, unless CharmFS gains access to some additional file metadata eventually.

This is a good thing in my book, but it'll have a (significant perhaps) performance impact and limit Rclone functionality when using a CharmFS remote.

## Testing

Run `make test`.

This will:

* Clone the rclone repository
* Add the CharmFS backend code
* Build the rclone binary
* Download a `charm` binary
* Start a charm server locally
* Run a few integration tests
