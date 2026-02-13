package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Format controls how output is rendered.
type Format string

const (
	FormatJSON  Format = "json"
	FormatHuman Format = "human"
)

// Writer handles structured output to stdout (data) and stderr (diagnostics).
type Writer struct {
	Format Format
	Out    io.Writer
	Err    io.Writer
	Quiet  bool
}

// Options for creating a Writer.
type Options struct {
	Format Format
	Out    io.Writer
	Err    io.Writer
	Quiet  bool
}

// New creates an output Writer.
func New(opts Options) *Writer {
	format := opts.Format
	if format == "" {
		format = FormatJSON
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	errOut := opts.Err
	if errOut == nil {
		errOut = os.Stderr
	}
	return &Writer{
		Format: format,
		Out:    out,
		Err:    errOut,
		Quiet:  opts.Quiet,
	}
}

// JSON writes a value as JSON to stdout. This is the primary data channel.
func (w *Writer) JSON(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w.Out, string(data))
	return err
}

// Emit writes structured output. In JSON mode, emits the value.
// In human mode, writes humanLines to stdout.
func (w *Writer) Emit(value any, humanLines []string) error {
	switch w.Format {
	case FormatHuman:
		if w.Quiet {
			return nil
		}
		for _, line := range humanLines {
			if _, err := fmt.Fprintln(w.Out, line); err != nil {
				return err
			}
		}
		return nil
	default:
		return w.JSON(value)
	}
}

// Infof writes a diagnostic message to stderr.
func (w *Writer) Infof(format string, args ...any) {
	if w.Quiet {
		return
	}
	_, _ = fmt.Fprintf(w.Err, format+"\n", args...)
}

// Errorf writes an error message to stderr.
func (w *Writer) Errorf(format string, args ...any) {
	_, _ = fmt.Fprintf(w.Err, "error: "+format+"\n", args...)
}

// ErrorJSON writes a structured error to stdout (for agent consumption).
func (w *Writer) ErrorJSON(errType string, message string, action string) error {
	payload := map[string]string{
		"error":   errType,
		"message": message,
	}
	if action != "" {
		payload["action"] = action
	}
	return w.JSON(payload)
}
