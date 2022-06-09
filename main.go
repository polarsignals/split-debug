package main

import (
	"debug/elf"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/polarsignals/split-debug/pkg/elfutils"
	"github.com/polarsignals/split-debug/pkg/elfwriter"
	"github.com/polarsignals/split-debug/pkg/logger"

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

var isDwarf = func(s *elf.Section) bool {
	return strings.HasPrefix(s.Name, ".debug_") ||
		strings.HasPrefix(s.Name, ".zdebug_") ||
		strings.HasPrefix(s.Name, "__debug_") // macos
}

var isSymbolTable = func(s *elf.Section) bool {
	return s.Name == ".symtab" || s.Name == ".strtab" || s.Name == ".dynsymtab"
}

var isGoSymbolTable = func(s *elf.Section) bool {
	return s.Name == ".gosymtab" || s.Name == ".gopclntab"
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

	for _, s := range elfFile.Sections {
		if isDwarf(s) || isSymbolTable(s) || isGoSymbolTable(s) {
			w.Sections = append(w.Sections, s)
		}
	}

	if err := w.Write(); err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed tom closer writer: %w", err)
	}
	return nil
}
