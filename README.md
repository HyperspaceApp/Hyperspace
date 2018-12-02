# [![Hyperspace Logo](http://i.imgur.com/9rxi9UV.png)](http://hspace.app) v0.2.3 (Rye)
----------------------------

[![Build Status](https://travis-ci.org/HyperspaceApp/Hyperspace.svg?branch=master)](https://travis-ci.org/HyperspaceApp/Hyperspace)
[![GoDoc](https://godoc.org/github.com/HyperspaceApp/Hyperspace?status.svg)](https://godoc.org/github.com/HyperspaceApp/Hyperspace)
[![Go Report Card](https://goreportcard.com/badge/github.com/HyperspaceApp/Hyperspace)](https://goreportcard.com/report/github.com/HyperspaceApp/Hyperspace)
[![License MIT](https://img.shields.io/badge/License-MIT-brightgreen.svg)](https://img.shields.io/badge/License-MIT-brightgreen.svg)

Hyperspace is a new decentralized cloud storage platform that radically alters the
landscape of cloud storage. By leveraging smart contracts, client-side
encryption, and sophisticated redundancy (via Reed-Solomon codes), Hyperspace allows
users to safely store their data with hosts that they do not know or trust.
The result is a cloud storage marketplace where hosts compete to offer the
best service at the lowest price. And since there is no barrier to entry for
hosts, anyone with spare storage capacity can join the network and start
making money.

![UI](http://i.imgur.com/SQeaXQ3.png)

Traditional cloud storage has a number of shortcomings. Users are limited to a
few big-name offerings: Google, Microsoft, Amazon. These companies have little
incentive to encrypt your data or make it easy to switch services later. Their
code is closed-source, and they can lock you out of your account at any time.

We believe that users should own their data. Hyperspace achieves this by replacing
the traditional monolithic cloud storage provider with a blockchain and a
swarm of hosts, each of which stores an encrypted fragment of your data. Since
the fragments are redundant, no single host can hold your data hostage: if
they jack up their price or go offline, you can simply download from a
different host. In other words, trust is removed from the equation, and
switching to a different host is painless. Stripped of these unfair
advantages, hosts must compete solely on the quality and price of the storage
they provide.

Hyperspace can serve as a replacement for personal backups, bulk archiving, content
distribution, and more. For developers, Hyperspace is a low-cost alternative to
Amazon S3. Storage on Hyperspace is a full order of magnitude cheaper than on S3,
with comparable bandwidth, latency, and durability. Hyperspace works best for static
content, especially media like videos, music, and photos.

Distributing data across many hosts automatically confers several advantages.
The most obvious is that, just like BitTorrent, uploads and downloads are
highly parallel. Given enough hosts, Hyperspace can saturate your bandwidth. Another
advantage is that your data is spread across a wide geographic area, reducing
latency and safeguarding your data against a range of attacks.

It is important to note that users have full control over which hosts they
use. You can tailor your host set for minimum latency, lowest price, widest
geographic coverage, or even a strict whitelist of IP addresses or public
keys.

At the core of Hyperspace is a blockchain that closely resembles Bitcoin.
Transactions are conducted in Hyperspacecoin, a cryptocurrency. The blockchain is
what allows Hyperspace to enforce its smart contracts without relying on centralized
authority.

Usage
-----

Hyperspace is ready for use with small sums of money and non-critical files, but
until the network has a more proven track record, we advise against using it
as a sole means of storing important data.

This release comes with 2 binaries, hsd and hsc. hsd is a background
service, or "daemon," that runs the Hyperspace protocol and exposes an HTTP API on
port 5580. hsc is a command-line client that can be used to interact with
hsd in a user-friendly way. There is also a graphical client, [Hyperspace.app](https://github.com/HyperspaceApp/Hyperspace.app), which
is the preferred way of using Hyperspace for most users. For interested developers,
the hsd API is documented [here](doc/API.md).

hsd and hsc are run via command prompt. On Windows, you can just double-
click hsd.exe if you don't need to specify any command-line arguments.
Otherwise, navigate to its containing folder and click File->Open command
prompt. Then, start the hsd service by entering `hsd` and pressing Enter.
The command prompt may appear to freeze; this means hsd is waiting for
requests. Windows users may see a warning from the Windows Firewall; be sure
to check both boxes ("Private networks" and "Public networks") and click
"Allow access." You can now run `hsc` (in a separate command prompt) or Hyperspace-
UI to interact with hsd. From here, you can send money, upload and download
files, and advertise yourself as a host.

Building From Source
--------------------

To build from source, [Go 1.10 must be installed](https://golang.org/doc/install)
on the system. Make sure your `$GOPATH` is set, and then simply use `go get`:

```
go get -u github.com/HyperspaceApp/Hyperspace/...
```

This will download the Hyperspace repo to your `$GOPATH/src` folder and install the
`hsd` and `hsc` binaries in your `$GOPATH/bin` folder.

To stay up-to-date, run the previous `go get` command again. Alternatively, you
can use the Makefile provided in this repo. Run `git pull origin master` to
pull the latest changes, and `make release` to build the new binaries. You
can also run `make test` and `make test-long` to run the short and full test
suites, respectively. Finally, `make cover` will generate code coverage reports
for each package; they are stored in the `cover` folder and can be viewed in
your browser.
