package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/kakkoyun/split-debug/pkg/elfutils"
	"github.com/kakkoyun/split-debug/pkg/elfwriter"
	"github.com/kakkoyun/split-debug/pkg/logger"

	"github.com/alecthomas/kong"
	"github.com/go-kit/log/level"
)

type flags struct {
	LogLevel string `kong:"enum='error,warn,info,debug',help='Log level.',default='info'"`
	Path     string `kong:"required,arg,name='path',help='File path to the object file extract debug information from.',type:'path'"`
}

func main() {
	flags := flags{}
	_ = kong.Parse(&flags)
	l := logger.NewLogger(flags.LogLevel, logger.LogFormatLogfmt, "")
	if err := run(flags.Path); err != nil {
		level.Error(l).Log("err", err)
		os.Exit(1)
	}
	level.Info(l).Log("msg", "done!")
}

func run(path string) error {
	elfFile, err := elfutils.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open given field: %w", err)
	}
	defer elfFile.Close()

	output, err := ioutil.TempFile(filepath.Dir(path), filepath.Base(path)+"-debuginfo.*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	w, err := elfwriter.New(output, &elfFile.FileHeader)
	if err != nil {
		return fmt.Errorf("failed to initialize writer: %w", err)
	}

	// TODO(kakkoyun): Remove executable code.
	// for _, p := range elfFile.Progs {
	// 	w.Progs = append(w.Progs, p)
	// }
	w.Progs = append(w.Progs, elfFile.Progs...)

	// TODO(kakkoyun): Filter debug information, strtab, etc.
	// for _, s := range elfFile.Sections {
	// 	w.Sections = append(w.Sections, s)
	// }
	w.Sections = append(w.Sections, elfFile.Sections...)

	// TODO(kakkoyun): Add example notes.

	if err := w.Write(); err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed tom closer writer: %w", err)
	}
	return nil
}
