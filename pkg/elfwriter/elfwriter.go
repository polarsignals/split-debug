// Package elfwriter is a package to write ELF files without having their entire
// contents in memory at any one time.
//
// Original work started from https://github.com/go-delve/delve/blob/master/pkg/elfwriter/writer.go
// and additional functionality added on top.
//
// This package does not provide completeness guarantees, only features needed to write core files are
// implemented, notably missing:
// - ?
package elfwriter

import (
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// TODO(kakkoyun): Fix entry point. Copy?
// TODO(kakkoyun): Make sure everything works when an overlapping chunk removed.
// TODO(kakkoyun): Sections to segment mapping.
// TODO(kakkoyun): Handle compressed sections. Keep compressed section compressed.
// fr := io.NewSectionReader(s.sr, s.compressionOffset, int64(s.FileSize)-s.compressionOffset)
// zlib.NewReader(fr)
// zlib.NewWriter

// TODO(kakkoyun): Make sure relocations work. Do we actually need to provide guarantees?

// WriteCloserSeeker is the union of io.Writer, io.Closer and io.Seeker.
type WriteCloserSeeker interface {
	io.Writer
	io.Seeker
	io.Closer
}

// Writer writes ELF files.
type Writer struct {
	w         WriteCloserSeeker
	byteOrder binary.ByteOrder
	class     elf.Class
	// TODO(kakkoyun): Data?

	Err error

	Progs    []*elf.Prog
	Sections []*elf.Section

	seekProgHeader       int64 // phoff
	seekProgNum          int64 // phnum
	seekSectionHeader    int64 // shoff
	seekSectionNum       int64 // shnun
	seekSectionStringIdx int64 // shstrndx

	shnum, shoff, shstrndx int64 // just for validation after Write.
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

	wrt := &Writer{
		w:         w,
		byteOrder: fhdr.ByteOrder,
		class:     fhdr.Class,
	}

	// TODO(kakkoyun): Check if this is still unsupported.
	// if fhdr.Data != elf.ELFDATA2LSB {
	// 	panic("unsupported")
	// }

	if err := wrt.writeFileHeader(fhdr); err != nil {
		return nil, fmt.Errorf("failed to write file header: %w", err)
	}
	return wrt, nil
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
	// 	- Code (.text)
	//  - Data (all other sections)
	//  - Sections' names (as last section)
	// 4. Section Header Table
	w.writeSegments()
	if w.Err != nil {
		return w.Err
	}
	w.writeSections()
	if w.Err != nil {
		return w.Err
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
		w.align(4) // TODO(kakkoyun): possible change the align value!
		w.write(note.Data)
	}

	var write func(note *Note)
	switch w.class {
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
func (w *Writer) writeFileHeader(fhdr *elf.FileHeader) error {
	var ehsize, phentsize, shentsize uint16
	switch fhdr.Class {
	case elf.ELFCLASS32:
		ehsize = 52
		phentsize = 32
		shentsize = 40
	case elf.ELFCLASS64:
		ehsize = 64
		phentsize = 56
		shentsize = 64
	default:
		return errors.New("unknown ELF class")
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

	switch w.class {
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
		w.u32(0)                    // e_entry // TODO(kakkoyun): !
		w.seekProgHeader = w.here()
		w.u32(0) // e_phoff
		w.seekSectionHeader = w.here()
		w.u32(0)         // e_shoff
		w.u32(0)         // e_flags
		w.u16(ehsize)    // e_ehsize
		w.u16(phentsize) // e_phentsize
		w.seekProgNum = w.here()
		w.u16(0) // e_phnum
		w.u16(0) // e_shentsize
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
		w.u64(0)                    // e_entry // TODO(kakkoyun): !
		w.seekProgHeader = w.here()
		w.u64(0) // e_phoff
		w.seekSectionHeader = w.here()
		w.u64(0)         // e_shoff
		w.u32(0)         // e_flags
		w.u16(ehsize)    // e_ehsize
		w.u16(phentsize) // e_phentsize
		w.seekProgNum = w.here()
		w.u16(0)         // e_phnum
		w.u16(shentsize) // e_shentsize
		w.seekSectionNum = w.here()
		w.u16(0) // e_shnum
		w.seekSectionStringIdx = w.here()
		w.u16(uint16(elf.SHN_UNDEF)) // e_shstrndx
	}

	// Sanity check, size of file header should be the same as ehsize
	if sz, _ := w.w.Seek(0, io.SeekCurrent); sz != int64(ehsize) {
		return errors.New("internal error, ELF header size")
	}
	return nil
}

// writeSegments writes the program headers at the current location
// and patches the file header accordingly.
func (w *Writer) writeSegments() {
	phoff := w.here()

	// Patch file header.
	w.seek(w.seekProgHeader, io.SeekStart)
	w.u64(uint64(phoff))
	w.seek(w.seekProgNum, io.SeekStart)
	w.u64(uint64(len(w.Progs))) // e_phnum
	w.seek(0, io.SeekEnd)

	write32 := func(prog *elf.Prog) {
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

	write64 := func(prog *elf.Prog) {
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
		w.u32(uint32(prog.Type))
		w.u32(uint32(prog.Flags))
		w.u64(prog.Off)
		w.u64(prog.Vaddr)
		w.u64(prog.Paddr)
		w.u64(prog.Filesz)
		w.u64(prog.Memsz)
		w.u64(prog.Align)
	}

	var write func(prog *elf.Prog)
	switch w.class {
	case elf.ELFCLASS32:
		write = write32
	case elf.ELFCLASS64:
		write = write64
	}

	for _, prog := range w.Progs {
		// Write program header to program header table.
		write(prog)
	}

	// TODO(kakkoyun): Add program/segment data.
	// iohelper.NewSectionWriter(w.w, int64(prog.Off), int64(prog.Filesz))
	// p.sr = io.NewSectionReader(r, int64(p.Off), int64(p.Filesz))
	// p.ReaderAt = p.sr
	// f.Progs[i] = p

	// TODO(kakkoyun): Data?
	// Write the segment
	//        _, err = w.Seek(int64(prog.Offset), io.SeekStart)
	//        if err != nil {
	//            return err
	//        }
	//        _, err = w.Write(prog.Data)
	//        if err != nil {
	//            return err
	//        }
	//
}

// writeSections writes the sections at the current location
// and patches the file header accordingly.
func (w *Writer) writeSections() {
	// 			   +-------------------+
	// 			   | ELF header        |---+
	// +---------> +-------------------+   | e_shoff
	// |           |                   |<--+
	// | Section   | Section header 0  |
	// |           |                   |---+ sh_offset
	// | Header    +-------------------+   |
	// |           | Section header 1  |---|--+ sh_offset
	// | Table     +-------------------+   |  |
	// |           | Section header 2  |---|--|--+
	// +---------> +-------------------+   |  |  |
	// 			   | Section 0         |<--+  |  |
	// 			   +-------------------+      |  | sh_offset
	// 			   | Section 1         |<-----+  |
	// 			   +-------------------+         |
	// 			   | Section 2         |<--------+
	// 			   +-------------------+
	shoff := w.here()
	shnum := len(w.Sections)

	// Patch file header.
	w.seek(w.seekSectionHeader, io.SeekStart)
	w.u64(uint64(shoff))
	w.shoff = shoff
	w.seek(w.seekSectionNum, io.SeekStart)
	w.u64(uint64(shnum) + 1) // e_shnum
	w.shnum = int64(shnum + 1)
	w.seek(w.seekSectionStringIdx, io.SeekStart)
	w.u64(uint64(shnum)) // The last section is string names.
	w.seek(0, io.SeekEnd)

	write32 := func(i int, sec *elf.Section) {
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
		w.u32(uint32(i))
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

	write64 := func(i int, sec *elf.Section) {
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
		w.u32(uint32(i))
		w.u32(uint32(sec.Type))
		w.u32(uint32(sec.Flags))
		w.u64(sec.Addr)
		w.u64(sec.Offset)
		w.u64(sec.Size)
		w.u32(sec.Link)
		w.u32(sec.Info)
		w.u64(sec.Addralign)
		w.u64(sec.Entsize)
	}

	var write func(i int, sec *elf.Section)
	switch w.class {
	case elf.ELFCLASS32:
		write = write32
	case elf.ELFCLASS64:
		write = write64
	}

	// TODO(kakkoyun): In index 0, SHT_NULL is mandatory.
	names := make([]string, shnum)
	for i, sec := range w.Sections {
		names[i] = sec.Name
		write(i, sec)
	}
	// Patch section name string table
	// Type: SHT_STRTAB
	// TODO(kakkoyun): Add a section header. But how?
	for _, nm := range names {
		w.write([]byte(nm)) // TODO(kakkoyun): String end/separator?
	}

	// TODO(kakkoyun): Add section data.
	// _, err = w.Seek(int64(sh.Offset), io.SeekStart)
	// if err != nil {
	//	return err
	// }
	// _, err = io.Copy(w, section.Open())
	// if err != nil {
	//	return err
	// }
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
	err := binary.Write(w.w, w.byteOrder, n)
	if err != nil && w.Err == nil {
		w.Err = err
	}
}

func (w *Writer) u32(n uint32) {
	err := binary.Write(w.w, w.byteOrder, n)
	if err != nil && w.Err == nil {
		w.Err = err
	}
}

func (w *Writer) u64(n uint64) {
	err := binary.Write(w.w, w.byteOrder, n)
	if err != nil && w.Err == nil {
		w.Err = err
	}
}
