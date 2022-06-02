package elfutils

import (
	"debug/elf"
	"fmt"
	"io"
	"os"
)

func Open(filePath string) (*elf.File, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening %s: %w", filePath, err)
	}
	defer f.Close()

	// Read the first 4 bytes of the file.
	var header [4]byte
	if _, err = io.ReadFull(f, header[:]); err != nil {
		return nil, fmt.Errorf("error reading magic number from %s: %w", filePath, err)
	}

	// Match against supported file types.
	if elfMagic := string(header[:]); elfMagic == elf.ELFMAG {
		f, err := elf.Open(filePath)
		if err != nil {
			return nil, fmt.Errorf("error reading ELF file %s: %w", filePath, err)
		}
		return f, nil
	}

	return nil, fmt.Errorf("unrecognized object file format: %s", filePath)
}
