package elfwriter

import (
	"debug/elf"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kakkoyun/split-debug/pkg/elfutils"
)

var isDwarf = func(s *elf.Section) bool {
	return strings.HasPrefix(s.Name, ".debug_") ||
		strings.HasPrefix(s.Name, ".zdebug_") ||
		strings.HasPrefix(s.Name, "__debug_") // macos
}

var isSymbolTable = func(s *elf.Section) bool {
	return s.Name == ".symtab" || s.Name == ".dynsymtab"
}

var isGoSymbolTable = func(s *elf.Section) bool {
	return s.Name == ".gosymtab" || s.Name == ".gopclntab"
}

func TestWriter_Write(t *testing.T) {
	inElf, err := elfutils.Open("../../dist/split-debug")
	require.NoError(t, err)
	t.Cleanup(func() {
		inElf.Close()
	})

	var secExceptDebug []*elf.Section
	for _, s := range inElf.Sections {
		if !isDwarf(s) {
			secExceptDebug = append(secExceptDebug, s)
		}
	}

	var secDebug []*elf.Section
	for _, s := range inElf.Sections {
		if isDwarf(s) || isSymbolTable(s) || isGoSymbolTable(s) {
			secDebug = append(secDebug, s)
		}
	}

	type fields struct {
		FileHeader *elf.FileHeader
		Progs      []*elf.Prog
		Sections   []*elf.Section
	}
	tests := []struct {
		name   string
		fields fields
		err    error
		// TODO(kakkoyun): Add expectations!
	}{
		// {
		// 	name: "only keep file header",
		// 	fields: fields{
		// 		FileHeader: &inElf.FileHeader,
		// 	},
		// 	err: nil,
		// },
		// {
		// 	name: "only keep program header",
		// 	fields: fields{
		// 		FileHeader: &inElf.FileHeader,
		// 		Progs:      inElf.Progs,
		// 	},
		// 	err: nil,
		// },
		{
			name: "keep all sections and segments",
			fields: fields{
				FileHeader: &inElf.FileHeader,
				Progs:      inElf.Progs,
				Sections:   inElf.Sections,
			},
			err: nil,
		},
		// {
		// 	name: "keep all sections except debug information",
		// 	fields: fields{
		// 		FileHeader: &inElf.FileHeader,
		// 		Sections:   secExceptDebug,
		// 	},
		// 	err: nil,
		// },
		// {
		// 	name: "keep only debug information",
		// 	fields: fields{
		// 		FileHeader: &inElf.FileHeader,
		// 		Sections:   secDebug,
		// 	},
		// 	err: nil,
		// },
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := ioutil.TempFile("", "test-output.*")
			require.NoError(t, err)
			t.Cleanup(func() {
				os.Remove(output.Name())
			})

			w, err := New(output, &inElf.FileHeader)
			require.NoError(t, err)
			w.Progs = append(w.Progs, tt.fields.Progs...)
			w.Sections = append(w.Sections, tt.fields.Sections...)

			err = w.Write()
			if tt.err != nil {
				require.EqualError(t, err, tt.err.Error())
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, w.Close())

			outElf, err := elfutils.Open(output.Name())
			require.NoError(t, err)

			t.Log(outElf.FileHeader)
			t.Log(outElf.Progs)
			t.Log(outElf.Sections)

			// oldshstrtab := inElf.Section(sectionHeaderStrTable)
			// newshstrtab := outElf.Section(sectionHeaderStrTable)
			//
			// olddata, err := oldshstrtab.Data()
			// require.NoError(t, err)
			//
			// newdata, err := newshstrtab.Data()
			// require.NoError(t, err)
			//
			// oldsplit := bytes.Split(olddata, []byte{0})
			// var notsection []string
			// for _, b := range oldsplit {
			// 	if !bytes.HasPrefix(b, []byte(".")) {
			// 		notsection = append(notsection, string(b))
			// 	}
			// }
			// t.Log("Not section:", notsection)
			// newsplit := bytes.Split(newdata, []byte{0})
			// require.Equal(t, oldsplit, newsplit)
			// require.Equal(t, olddata, newdata)

			// TODO(kakkoyun): Compare: readelf -h outputs.
			// TODO(kakkoyun): Compare: readelf -SW outputs.
			// TODO(kakkoyun): readelf -e (Equivalent to: -h -l -S)
		})
	}
}
