[![Apache 2 License](https://img.shields.io/badge/license-Apache%202-blue.svg)](LICENSE)
[![Maintenance](https://img.shields.io/maintenance/yes/2022.svg)](https://github.com/kakkoyun/split-debug/commits/master) ![build](https://github.com/kakkoyun/split-debug/actions/workflows/build.yml/badge.svg)

[![Latest Release](https://img.shields.io/github/release/kakkoyun/split-debug.svg)](https://github.com/kakkoyun/split-debug/releases/latest) ![release](https://github.com/kakkoyun/split-debug/actions/workflows/release.yml/badge.svg)

[![Go Code reference](https://img.shields.io/badge/code%20reference-go.dev-darkblue.svg)](https://pkg.go.dev/github.com/kakkoyun/split-debug?tab=subdirectories) [![Go Report Card](https://goreportcard.com/badge/github.com/kakkoyun/split-debug)](https://goreportcard.com/report/github.com/kakkoyun/split-debug)

# split-debug

A native Golang tool to extract DWARF and Symbol information for ELF Object files

## TODO

* [] Ensure consistency of linked sections when target removed (sh_link)
* [] Ensure consistency and existence of overlapping segments when a section removed (offset, range check)
* [] Ensure consistency and soundness of relocations (type: SHT_RELA)
* [] Ensure soundness of entry point (if the output ELF file is still executable) 

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
