package elfwriter

type Option func(w *Writer)

func WithDebugCompressionEnabled(b bool) Option {
	return func(w *Writer) {
		w.debugCompressionEnabled = b
	}
}
