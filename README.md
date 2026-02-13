
# sprync: the missing file transfer command for sprites

```
NAME:
   sprync - sync directories with Sprite VMs

USAGE:
   sprync [global options] command [command options]

COMMANDS:
   push     push directory to sprite
   pull     pull sprite directory to local
   diff     show what push or pull would do
   doctor   verify sprite connectivity
   version  print version
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --token value    Sprite API token [$SPRITE_TOKEN]
   --api value      Sprite API base URL (default: "https://api.sprites.dev")
   --timeout value  operation timeout (default: 5m0s)
   --verbose, -v    verbose output (default: false)
   --help, -h       show help
```

## Fts. 

* Copies only what's changed (about 67% as smart as rsync)

* Files or directory trees, preserving attributes

* Copy from sprite->macbook, macbook->sprite, sprite->sprite

* Sprite->sprite copies don't run bulk data through your macbook

* Reasonably RTT-efficient

## Design

We upload a stager (`spryncd`, embedded in `sprync`) to a tempfile on the Sprite.

An `exec` control channel builds manifests on both sides of the connection; the 
sender builds a tarball of changes and pushes it in a single FS operation.

We make only minimal use of the FS API, because it is slow and Kurt should feel bad.

