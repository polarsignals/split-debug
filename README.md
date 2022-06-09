[![Apache 2 License](https://img.shields.io/badge/license-Apache%202-blue.svg)](LICENSE)
[![Maintenance](https://img.shields.io/maintenance/yes/2022.svg)](https://github.com/polarsignals/split-debug/commits/master) ![build](https://github.com/polarsignals/split-debug/actions/workflows/build.yml/badge.svg)

[![Latest Release](https://img.shields.io/github/release/polarsignals/split-debug.svg)](https://github.com/polarsignals/split-debug/releases/latest) ![release](https://github.com/polarsignals/split-debug/actions/workflows/release.yml/badge.svg)

[![Go Code reference](https://img.shields.io/badge/code%20reference-go.dev-darkblue.svg)](https://pkg.go.dev/github.com/polarsignals/split-debug?tab=subdirectories) [![Go Report Card](https://goreportcard.com/badge/github.com/polarsignals/split-debug)](https://goreportcard.com/report/github.com/polarsignals/split-debug)

# split-debug

A native Golang tool to extract DWARF and Symbol information for ELF Object files

# DISCLAIMER

> This project is in-complete.

But feel free to contribute. This project is a proof of concept for https://github.com/parca-dev/parca-agent to extract debug information from ELF files using pure Go.
It turns out a fully-fledged ELF writer written in Go doesn't exist.

- https://www.reddit.com/r/golang/comments/v1v9yp/looking_for_a_package_for_writing_elf_files/iap3fx8/?context=3
- https://twitter.com/kpolarsignals/status/1531688277740240896?s=20&t=n6T5PmNNQmvAtKWxSiwjMA

So I started to write a package for that. I'm not an expert on the format, but I'm learning. Please feel free to contribute.

## TODO

* [ ] Ensure consistency of linked sections when target removed (sh_link)
* [ ] Ensure consistency and existence of overlapping segments when a section removed (offset, range check)
* [ ] Ensure consistency and soundness of relocations (type: SHT_RELA)
* [ ] Ensure soundness of entry point (if the output ELF file is still executable) 

## Configuration

Flags:

[embedmd]:# (dist/help.txt)
```txt
Usage: split-debug <path>

Arguments:
  <path>    File path to the object file extract debug information from.

Flags:
  -h, --help                Show context-sensitive help.
      --log-level="info"    Log level.
```
