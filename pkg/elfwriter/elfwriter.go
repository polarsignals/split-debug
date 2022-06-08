// Package elfwriter is a package to write ELF files without having their entire
// contents in memory at any one time.
//
// Original work started from https://github.com/go-delve/delve/blob/master/pkg/elfwriter/writer.go
// and additional functionality added on top.
//
// This package does not provide completeness guarantees, only features needed to write core files are
// implemented, notably missing:
// - Consistency and soundness of relocations (type: SHT_RELA)
// - Consistency of linked sections when target removed (sh_link)
// - Consistency and existence of overlapping segments when a section removed (offset, range check)
package elfwriter

import (
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"runtime/debug"

	"golang.org/x/sys/unix"
)

// TODO(kakkoyun): Fix entry point. Copy?
// TODO(kakkoyun): Make sure everything works when an overlapping chunk removed.
// TODO(kakkoyun): Check if sections to segment mapping created properly.
// TODO(kakkoyun): Handle compressed sections. Keep compressed section compressed.
//  - fr := io.NewSectionReader(s.sr, s.compressionOffset, int64(s.FileSize)-s.compressionOffset)
//  - zlib.NewReader(fr)
//  - zlib.NewWriter

// TODO(kakkoyun): Make sure relocations work. Do we actually need to provide guarantees?

const sectionHeaderStrTable = ".shstrtab"

// WriteCloserSeeker is the union of io.Writer, io.Closer and io.Seeker.
type WriteCloserSeeker interface {
	io.Writer
	io.Seeker
	io.Closer
}

// Writer writes ELF files.
type Writer struct {
	w    WriteCloserSeeker
	fhdr *elf.FileHeader

	Err error

	Progs    []*elf.Prog
	Sections []*elf.Section
	shStrIdx map[string]int

	seekProgHeader       int64 // position of phoff
	seekProgNum          int64 // position of phnum
	seekSectionHeader    int64 // position of shoff
	seekSectionNum       int64 // position of shnun
	seekSectionStringIdx int64 // position of shstrndx
	seekSectionEntrySize int64

	// For validation.
	ehsize, phentsize, shentsize uint16
	shnum, shoff, shstrndx       int64
}

type Note struct {
	Type elf.NType
	Name string
	Data []byte
}

// New creates a new Writer.
func New(w WriteCloserSeeker, fhdr *elf.FileHeader) (*Writer, error) {
	if fhdr.ByteOrder == nil {
		return nil, errors.New("byte order has to be specified")
	}

	switch fhdr.Class {
	case elf.ELFCLASS32:
	case elf.ELFCLASS64:
		// Ok
	default:
		return nil, errors.New("unknown ELF class")
	}

	// TODO(kakkoyun): Check if this is still unsupported.
	// if fhdr.Data != elf.ELFDATA2LSB {
	// 	panic("unsupported")
	// }
	return &Writer{
		w:        w,
		fhdr:     fhdr,
		shStrIdx: make(map[string]int),
	}, nil
}

// Write writes the segments (program headers) and sections to output.
func (w *Writer) Write() error {
	// +-------------------------------+
	// | ELF File Header               |
	// +-------------------------------+
	// | Program Header for segment #1 |
	// +-------------------------------+
	// | Program Header for segment #2 |
	// +-------------------------------+
	// | ...                           |
	// +-------------------------------+
	// | Contents (Byte Stream)        |
	// | ...                           |
	// +-------------------------------+
	// | Section Header for section #1 |
	// +-------------------------------+
	// | Section Header for section #2 |
	// +-------------------------------+
	// | ...                           |
	// +-------------------------------+
	// | ".shstrtab" section           |
	// +-------------------------------+
	// | ".symtab"   section           |
	// +-------------------------------+
	// | ".strtab"   section           |
	// +-------------------------------+

	// 1. File Header (written in .New())
	// 2. Program Header Table
	// 3. Sections
	// 4. Section Header Table
	w.writeFileHeader()
	if w.Err != nil {
		return fmt.Errorf("failed to write file header: %w", w.Err)
	}
	if len(w.Progs) > 0 {
		w.writeSegments()
	}
	if w.Err != nil {
		return fmt.Errorf("failed to write segments: %w", w.Err)
	}
	if len(w.Sections) > 0 {
		w.writeSections()
	}
	if w.Err != nil {
		return fmt.Errorf("failed to write sections: %w", w.Err)
	}

	if w.shoff == 0 && w.shnum != 0 {
		return fmt.Errorf("invalid ELF shnum=%d for shoff=0", w.shnum)
	}
	if w.shnum > 0 && w.shstrndx >= w.shnum {
		return fmt.Errorf("invalid ELF shstrndx=%d", w.shstrndx)
	}
	return nil
}

// WriteNotes writes notes to the current location, returns a ProgHeader describing the
// notes.
func (w *Writer) WriteNotes(notes []Note) *elf.ProgHeader {
	if len(notes) == 0 {
		return nil
	}
	h := &elf.ProgHeader{
		Type:  elf.PT_NOTE,
		Align: 4,
	}

	write32 := func(note *Note) {
		// Note header in a PT_NOTE section
		// typedef struct elf32_note {
		//   Elf32_Word	n_namesz;	/* Name size */
		//   Elf32_Word	n_descsz;	/* Content size */
		//   Elf32_Word	n_type;		/* Content type */
		// } Elf32_Nhdr;
		//
		w.align(4)
		if h.Off == 0 {
			h.Off = uint64(w.here())
		}
		w.u32(uint32(len(note.Name)))
		w.u32(uint32(len(note.Data)))
		w.u32(uint32(note.Type))
		w.write([]byte(note.Name))
		w.align(4)
		w.write(note.Data)
	}

	write64 := func(note *Note) {
		// Note header in a PT_NOTE section
		// typedef struct elf64_note {
		//   Elf64_Word n_namesz;	/* Name size */
		//   Elf64_Word n_descsz;	/* Content size */
		//   Elf64_Word n_type;	/* Content type */
		// } Elf64_Nhdr;
		w.align(4)
		if h.Off == 0 {
			h.Off = uint64(w.here())
		}
		w.u64(uint64(len(note.Name)))
		w.u64(uint64(len(note.Data)))
		w.u64(uint64(note.Type))
		w.write([]byte(note.Name))
		w.align(4) // TODO(kakkoyun): 4 or 8. Possibly needs change! Check the value in the file header?
		w.write(note.Data)
	}

	var write func(note *Note)
	switch w.fhdr.Class {
	case elf.ELFCLASS32:
		write = write32
	case elf.ELFCLASS64:
		write = write64
	}

	for i := range notes {
		write(&notes[i])
	}
	h.Filesz = uint64(w.here()) - h.Off
	return h
}

// writeFileHeader writes the initial file header using given information.
func (w *Writer) writeFileHeader() {
	fhdr := w.fhdr

	switch fhdr.Class {
	case elf.ELFCLASS32:
		w.ehsize = 52
		w.phentsize = 32
		w.shentsize = 40
	case elf.ELFCLASS64:
		w.ehsize = 64
		w.phentsize = 56
		w.shentsize = 64
	default:
		w.Err = errors.New("unknown ELF class")
		return
	}

	// e_ident
	w.write([]byte{
		0x7f, 'E', 'L', 'F', // Magic number
		byte(fhdr.Class),
		byte(fhdr.Data),
		byte(fhdr.Version),
		byte(fhdr.OSABI),
		fhdr.ABIVersion,
		0, 0, 0, 0, 0, 0, 0, // Padding
	})

	switch fhdr.Class {
	case elf.ELFCLASS32:
		// type Header32 struct {
		// 	Ident     [EI_NIDENT]byte /* File identification. */
		// 	Type      uint16          /* File type. */
		// 	Machine   uint16          /* Machine architecture. */
		// 	Version   uint32          /* ELF format version. */
		// 	Entry     uint32          /* Entry point. */
		// 	Phoff     uint32          /* Program header file offset. */
		// 	Shoff     uint32          /* Section header file offset. */
		// 	Flags     uint32          /* Architecture-specific flags. */
		// 	Ehsize    uint16          /* Size of ELF header in bytes. */
		// 	Phentsize uint16          /* Size of program header entry. */
		// 	Phnum     uint16          /* Number of program header entries. */
		// 	Shentsize uint16          /* Size of section header entry. */
		// 	Shnum     uint16          /* Number of section header entries. */
		// 	Shstrndx  uint16          /* Section name strings section. */
		// }
		w.u16(uint16(fhdr.Type))    // e_type
		w.u16(uint16(fhdr.Machine)) // e_machine
		w.u32(uint32(fhdr.Version)) // e_version
		w.u32(0)                    // e_entry // TODO(kakkoyun): Shall we clone from input? Any consistency checks?
		w.seekProgHeader = w.here()
		w.u32(0) // e_phoff
		w.seekSectionHeader = w.here()
		w.u32(0)           // e_shoff
		w.u32(0)           // e_flags
		w.u16(w.ehsize)    // e_ehsize
		w.u16(w.phentsize) // e_phentsize
		w.seekProgNum = w.here()
		w.u16(0) // e_phnum
		w.seekSectionEntrySize = w.here()
		w.u16(w.shentsize) // e_shentsize
		w.seekSectionNum = w.here()
		w.u16(0) // e_shnum
		w.seekSectionStringIdx = w.here()
		w.u16(uint16(elf.SHN_UNDEF)) // e_shstrndx
	case elf.ELFCLASS64:
		// type Header64 struct {
		// 	Ident     [EI_NIDENT]byte /* File identification. */
		// 	Type      uint16          /* File type. */
		// 	Machine   uint16          /* Machine architecture. */
		// 	Version   uint32          /* ELF format version. */
		// 	Entry     uint64          /* Entry point. */
		// 	Phoff     uint64          /* Program header file offset. */
		// 	Shoff     uint64          /* Section header file offset. */
		// 	Flags     uint32          /* Architecture-specific flags. */
		// 	Ehsize    uint16          /* Size of ELF header in bytes. */
		// 	Phentsize uint16          /* Size of program header entry. */
		// 	Phnum     uint16          /* Number of program header entries. */
		// 	Shentsize uint16          /* Size of section header entry. */
		// 	Shnum     uint16          /* Number of section header entries. */
		// 	Shstrndx  uint16          /* Section name strings section. */
		// }
		w.u16(uint16(fhdr.Type))    // e_type
		w.u16(uint16(fhdr.Machine)) // e_machine
		w.u32(uint32(fhdr.Version)) // e_version
		w.u64(0)                    // e_entry // TODO(kakkoyun): Shall we clone from input? Any consistency checks?
		w.seekProgHeader = w.here()
		w.u64(0) // e_phoff
		w.seekSectionHeader = w.here()
		w.u64(0)           // e_shoff
		w.u32(0)           // e_flags
		w.u16(w.ehsize)    // e_ehsize
		w.u16(w.phentsize) // e_phentsize
		w.seekProgNum = w.here()
		w.u16(0) // e_phnum
		w.seekSectionEntrySize = w.here()
		w.u16(w.shentsize) // e_shentsize
		w.seekSectionNum = w.here()
		w.u16(0) // e_shnum
		w.seekSectionStringIdx = w.here()
		w.u16(uint16(elf.SHN_UNDEF)) // e_shstrndx
	}

	// Sanity check, size of file header should be the same as ehsize
	if sz, _ := w.w.Seek(0, io.SeekCurrent); sz != int64(w.ehsize) {
		w.Err = errors.New("internal error, ELF header size")
	}
}

// writeSegments writes the program headers at the current location
// and patches the file header accordingly.
func (w *Writer) writeSegments() {
	phoff := w.here()
	phnum := uint64(len(w.Progs))

	// Patch file header.
	w.seek(w.seekProgHeader, io.SeekStart)
	w.u64(uint64(phoff))
	w.seek(w.seekProgNum, io.SeekStart)
	w.u64(phnum) // e_phnum
	w.seek(0, io.SeekEnd)

	writePH32 := func(prog *elf.Prog) {
		// ELF32 Program header.
		// type Prog32 struct {
		// 	Type   uint32 /* Entry type. */
		// 	Off    uint32 /* File offset of contents. */
		// 	Vaddr  uint32 /* Virtual address in memory image. */
		// 	Paddr  uint32 /* Physical address (not used). */
		// 	Filesz uint32 /* Size of contents in file. */
		// 	Memsz  uint32 /* Size of contents in memory. */
		// 	Flags  uint32 /* Access permission flags. */
		// 	Align  uint32 /* Alignment in memory and file. */
		// }
		w.u32(uint32(prog.Type))
		w.u32(uint32(prog.Flags))
		w.u32(uint32(prog.Off))
		w.u32(uint32(prog.Vaddr))
		w.u32(uint32(prog.Paddr))
		w.u32(uint32(prog.Filesz))
		w.u32(uint32(prog.Memsz))
		w.u32(uint32(prog.Align))
	}

	writePH64 := func(prog *elf.Prog) {
		// ELF64 Program header.
		// type Prog64 struct {
		// 	Type   uint32 /* Entry type. */
		// 	Flags  uint32 /* Access permission flags. */
		// 	Off    uint64 /* File offset of contents. */
		// 	Vaddr  uint64 /* Virtual address in memory image. */
		// 	Paddr  uint64 /* Physical address (not used). */
		// 	Filesz uint64 /* Size of contents in file. */
		// 	Memsz  uint64 /* Size of contents in memory. */
		// 	Align  uint64 /* Alignment in memory and file. */
		// }
		w.u32(uint32(prog.Type)) // TODO(kakkoyun): Why first element is null?
		w.u32(uint32(prog.Flags))
		w.u64(prog.Off)
		w.u64(prog.Vaddr)
		w.u64(prog.Paddr)
		w.u64(prog.Filesz)
		w.u64(prog.Memsz)
		w.u64(prog.Align)
	}

	var writeProgramHeader func(prog *elf.Prog)
	switch w.fhdr.Class {
	case elf.ELFCLASS32:
		writeProgramHeader = writePH32
	case elf.ELFCLASS64:
		writeProgramHeader = writePH64
	}

	for _, prog := range w.Progs {
		// Write program header to program header table.
		writeProgramHeader(prog)
	}

	// TODO(kakkoyun): Add program/segment data.
	// for _, prog := range w.Progs {
	// 	// TODO(kakkoyun): Calculate using phentsize and update program header table.
	// 	prog.Off = uint64(w.here())
	// 	w.writeFrom(prog.Open())
	// 	// TODO(kakkoyun): Calculate using phentsize and update program header table.
	// 	prog.Filesz = uint64(w.here()) - prog.Off
	// 	// Unless the section is not compressed, the Memsz and Filesz is the same.
	// 	prog.Memsz = prog.Filesz
	// }
}

// writeSections writes the sections at the current location
// and patches the file header accordingly.
func (w *Writer) writeSections() {
	// http://www.sco.com/developers/gabi/2003-12-17/ch4.sheader.html
	// 			   +-------------------+
	// 			   | ELF header        |---+  e_shoff
	// 			   +-------------------+   |
	// 			   | Section 0         |<-----+
	// 			   +-------------------+   |  | sh_offset
	// 			   | Section 1         |<--|-----+
	// 			   +-------------------+   |  |  |
	// 			   | Section 2         |<--|--|--|--+
	// +---------> +-------------------+   |  |  |  |
	// |           |                   |<--+  |  |  |
	// | Section   | Section header 0  |      |  |  |
	// |           |                   |<-----+  |  |
	// | Header    +-------------------+         |  |
	// |           | Section header 1  |<--------+  |
	// | Table     +-------------------+            |
	// |           | Section header 2  |------------+ sh_offset
	// +---------> +-------------------+

	// Shallow copy the section for further editing.
	copy := func(s *elf.Section) *elf.Section {
		clone := new(elf.Section)
		*clone = *s
		return clone
	}

	// sections that will end up in the output.
	stw := make([]*elf.Section, 0, len(w.Sections)+2)

	// Build section header string table.
	shstrtab := new(elf.Section)
	shstrtab.Name = sectionHeaderStrTable
	shstrtab.Type = elf.SHT_STRTAB
	shstrtab.Addralign = 1

	for i, sec := range w.Sections {
		if i == 0 {
			if sec.Type == elf.SHT_NULL {
				stw = append(stw, copy(sec))
				continue
			}
			s := new(elf.Section)
			s.Type = elf.SHT_NULL
			stw = append(stw, s)
		}
		if sec.Type == elf.SHT_STRTAB && sec.Name == sectionHeaderStrTable {
			// Add new shstrtab, preserve order.
			stw = append(stw, shstrtab)
			w.shstrndx = int64(len(stw) - 1)
			continue
		}
		stw = append(stw, copy(sec))
	}

	shnum := len(stw)
	w.shnum = int64(shnum)

	names := make([]string, shnum)
	for i, sec := range stw {
		if sec.Name != "" {
			names[i] = sec.Name
		}
	}

	// Start writing actual data for sections.
	for i, sec := range stw {
		sec.Offset = uint64(w.here())
		// The section header string section is reserved for section header string table.
		if i == int(w.shstrndx) {
			w.writeStrtab(names)
		} else {
			// TODO(kakkoyun): Handle compression.
			w.writeFrom(sec.Open())
		}
		sec.FileSize = uint64(w.here()) - sec.Offset
		// Unless the section is not compressed, the Size and FileSize is the same.
		sec.Size = sec.FileSize
	}

	// Start writing the section header table.
	shoff := w.here()
	w.shoff = shoff
	// First, patch file header.
	w.seek(w.seekSectionHeader, io.SeekStart)
	w.u64(uint64(shoff))
	w.seek(w.seekSectionNum, io.SeekStart)
	w.u64(uint64(shnum)) // e_shnum
	w.seek(w.seekSectionStringIdx, io.SeekStart)
	w.u64(uint64(w.shstrndx))
	w.seek(w.seekSectionEntrySize, io.SeekStart)
	w.u16(w.shentsize) // e_shentsize
	w.seek(0, io.SeekEnd)

	writeSH32 := func(shstrndx int, sec *elf.Section) {
		// ELF32 Section header.
		// type Section32 struct {
		// 	Name      uint32 /* Section name (index into the section header string table). */
		// 	Type      uint32 /* Section type. */
		// 	Flags     uint32 /* Section flags. */
		// 	Addr      uint32 /* Address in memory image. */
		// 	Off       uint32 /* Offset in file. */
		// 	Size      uint32 /* Size in bytes. */
		// 	Link      uint32 /* Index of a related section. */
		// 	Info      uint32 /* Depends on section type. */
		// 	Addralign uint32 /* Alignment in bytes. */
		// 	Entsize   uint32 /* Size of each entry in section. */
		// }
		w.u32(uint32(shstrndx))
		w.u32(uint32(sec.Type))
		w.u32(uint32(sec.Flags))
		w.u32(uint32(sec.Addr))
		w.u32(uint32(sec.Offset))
		w.u32(uint32(sec.Size))
		w.u32(sec.Link)
		w.u32(sec.Info)
		w.u32(uint32(sec.Addralign))
		w.u32(uint32(sec.Entsize))
	}

	writeSH64 := func(shstrndx int, sec *elf.Section) {
		// ELF64 Section header.
		// type Section64 struct {
		// 	Name      uint32 /* Section name (index into the section header string table). */
		// 	Type      uint32 /* Section type. */
		// 	Flags     uint64 /* Section flags. */
		// 	Addr      uint64 /* Address in memory image. */
		// 	Off       uint64 /* Offset in file. */
		// 	Size      uint64 /* Size in bytes. */
		// 	Link      uint32 /* Index of a related section. */
		// 	Info      uint32 /* Depends on section type. */
		// 	Addralign uint64 /* Alignment in bytes. */
		// 	Entsize   uint64 /* Size of each entry in section. */
		// }
		w.u32(uint32(shstrndx))
		w.u32(uint32(sec.Type))
		w.u64(uint64(sec.Flags))
		w.u64(sec.Addr)
		w.u64(sec.Offset)
		w.u64(sec.Size)
		w.u32(sec.Link)
		w.u32(sec.Info)
		w.u64(sec.Addralign)
		w.u64(sec.Entsize)
	}

	// shstrndx index of the entry in the section header string table.
	// 0 reserved for null string.
	var writeSectionHeader func(shstrndx int, sec *elf.Section)
	switch w.fhdr.Class {
	case elf.ELFCLASS32:
		writeSectionHeader = writeSH32
	case elf.ELFCLASS64:
		writeSectionHeader = writeSH64
	}

	for _, sec := range stw {
		if sec.Name == "" {
			writeSectionHeader(0, sec)
			continue
		}
		writeSectionHeader(w.shStrIdx[sec.Name], sec)
	}
}

// Close closes the WriteCloseSeeker.
func (w *Writer) Close() error {
	var err error
	if w.w != nil {
		err = w.w.Close()
	}
	return err
}

// here returns the current seek offset from the start of the file.
func (w *Writer) here() int64 {
	r, err := w.w.Seek(0, io.SeekCurrent)
	if err != nil && w.Err == nil {
		w.Err = err
	}
	return r
}

// seek moves the cursor to the point calculated using offset and starting point.
func (w *Writer) seek(offset int64, whence int) {
	_, err := w.w.Seek(offset, whence)
	if err != nil && w.Err == nil {
		w.Err = err
	}
}

// align writes as many padding bytes as needed to make the current file
// offset a multiple of align.
func (w *Writer) align(align int64) {
	off := w.here()
	alignOff := (off + (align - 1)) &^ (align - 1)
	if alignOff-off > 0 {
		w.write(make([]byte, alignOff-off))
	}
}

func (w *Writer) write(buf []byte) {
	_, err := w.w.Write(buf)
	if err != nil && w.Err == nil {
		w.Err = err
	}
}

func (w *Writer) u16(n uint16) {
	err := binary.Write(w.w, w.fhdr.ByteOrder, n)
	if err != nil && w.Err == nil {
		w.Err = err
	}
}

func (w *Writer) u32(n uint32) {
	err := binary.Write(w.w, w.fhdr.ByteOrder, n)
	if err != nil && w.Err == nil {
		w.Err = err
	}
}

func (w *Writer) u64(n uint64) {
	err := binary.Write(w.w, w.fhdr.ByteOrder, n)
	if err != nil && w.Err == nil {
		w.Err = err
	}
}

// writeStrtab writes given strings in string table format.
func (w *Writer) writeStrtab(strs []string) {
	// http://www.sco.com/developers/gabi/2003-12-17/ch4.strtab.html
	w.write([]byte{0})
	i := 1
	for _, s := range strs {
		if s != "" {
			data, err := unix.ByteSliceFromString(s)
			if err != nil && w.Err == nil {
				w.Err = err
				break
			}
			w.shStrIdx[s] = i
			w.write(data)
			i += len(data)
		}
	}
}

func (w *Writer) writeFrom(r io.Reader) {
	if r == nil {
		w.Err = errors.New("reader is nil")
		return
	}

	pr, pw := io.Pipe()

	// write in writer end of pipe.
	var wErr error
	go func() {
		defer pw.Close()
		defer func() {
			if r := recover(); r != nil {
				debug.PrintStack()
				wErr = fmt.Errorf("panic occured: %w", r.(error))
			}
		}()
		_, wErr = io.Copy(pw, r)
	}()

	// read from reader end of pipe.
	defer pr.Close()
	_, rErr := io.Copy(w.w, pr)
	if wErr != nil && w.Err == nil {
		w.Err = wErr
	}
	if rErr != nil && w.Err == nil {
		w.Err = rErr
	}
}
