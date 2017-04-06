# Docker volume plugin for Ceph RBD

This plugin allows you to mount Ceph RBD volumes in your container easily.

[![Go Report Card](https://goreportcard.com/badge/github.com/djmaze/docker-volume-ceph-rbd)](https://goreportcard.com/report/github.com/djmaze/docker-volume-ceph-rbd)

Note: The code base is still very rough around the edges. It uses the kernel RBD interface directly and shells out where it is not needed.

Also, it does not yet support volume locking. That means it does not prevent attaching one RBD volume to multiple containers at the same time. There be dragons!

## Usage

1 - Install the plugin

```
$ docker plugin install  # or docker plugin install djmaze/ceph-rbd DEBUG=1
```

2 - Create a volume

```
$ docker volume create -d djmaze/ceph-rbd -o hosts=<ip1,ip2,..> -o pool=<poolname> -o rbd=<rbdname> -o username=<username> -o secret=<secret> rbdvolume
rbdvolume
$ docker volume ls
DRIVER              VOLUME NAME
local               2d75de358a70ba469ac968ee852efd4234b9118b7722ee26a1c5a90dcaea6751
local               842a765a9bb11e234642c933b3dfc702dee32b73e0cf7305239436a145b89017
local               9d72c664cbd20512d4e3d5bb9b39ed11e4a632c386447461d48ed84731e44034
local               be9632386a2d396d438c9707e261f86fd9f5e72a7319417901d84041c8f14a4d
local               e1496dfe4fa27b39121e4383d1b16a0a7510f0de89f05b336aab3c0deb4dda0e
djmaze/ceph-rbd     rbdvolume
```

3 - Use the volume

```
$ docker run -it -v rbdvolume:<path> busybox ls <path>
```

## THANKS

This project is a fork of [vieux/docker-volume-sshfs](https://github.com/vieux/docker-volume-sshfs), an excellent example on how to write a Docker volume driver.

https://github.com/vieux/docker-volume-sshfs
https://github.com/docker/go-plugins-helpers

## LICENSE

MIT
