package elfwriter

import (
	"debug/elf"
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kakkoyun/split-debug/pkg/elfutils"
)

func TestWriter_Write(t *testing.T) {
	type fields struct {
		Progs    []*elf.Prog
		Sections []*elf.Section
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := ioutil.TempFile("", "test-output.*")
			require.NoError(t, err)

			elfFile, err := elfutils.Open("../split-debug")
			require.NoError(t, err)
			t.Cleanup(func() {
				elfFile.Close()
			})

			w, err := New(output, &elfFile.FileHeader)
			require.NoError(t, err)

			// w := &Writer{
			// 	Progs:    tt.fields.Progs,
			// 	Sections: tt.fields.Sections,
			// }

			require.NoError(t, w.Write())
			require.NoError(t, w.Close())
			// TODO(kakkoyun): Compare: readelf -h outputs.
			// TODO(kakkoyun): Compare: readelf -SW outputs.
			// TODO(kakkoyun): readelf -e (Equivalent to: -h -l -S)
		})
	}
}
