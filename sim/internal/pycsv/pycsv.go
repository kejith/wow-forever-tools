// Package pycsv reads text the way Python's csv module and its universal newlines do, so that the Go side of
// the bridge splits a combat log or a DB2 table exactly as the scripts it replaces.
package pycsv

import "strings"

// SplitLines splits text as Python's universal newlines with newline="" do: a line ends after "\r\n", "\n"
// or "\r", and keeps its ending.
func SplitLines(text string) []string {
	var out []string
	start := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\n':
			out = append(out, text[start:i+1])
			start = i + 1
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			out = append(out, text[start:i+1])
			start = i + 1
		}
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}

// Reader is Python's csv.reader with the default dialect (CPython's _csv.c), fed one line at a time. Each
// line ends its record, unless a quoted field goes on into the next line.
type Reader struct {
	fields []string
	field  strings.Builder
	state  int
	any    bool // the field has a character (CPython's field_len)
}

const (
	startRecord = iota
	startField
	inField
	inQuoted
	quoteInQuoted
	eatCRNL
)

func (r *Reader) save() {
	r.fields = append(r.fields, r.field.String())
	r.field.Reset()
	r.any = false
}

func (r *Reader) add(c rune) {
	r.field.WriteRune(c)
	r.any = true
}

func (r *Reader) char(c rune) {
	newline := c == '\n' || c == '\r'
	switch r.state {
	case startRecord:
		if newline {
			r.state = eatCRNL
			return
		}
		r.state = startField
		fallthrough
	case startField:
		switch {
		case newline:
			r.save()
			r.state = eatCRNL
		case c == '"':
			r.state = inQuoted
		case c == ',':
			r.save()
		default:
			r.add(c)
			r.state = inField
		}
	case inField:
		switch {
		case newline:
			r.save()
			r.state = eatCRNL
		case c == ',':
			r.save()
			r.state = startField
		default:
			r.add(c)
		}
	case inQuoted:
		if c == '"' {
			r.state = quoteInQuoted
		} else {
			r.add(c)
		}
	case quoteInQuoted:
		switch {
		case c == '"':
			r.add('"')
			r.state = inQuoted
		case c == ',':
			r.save()
			r.state = startField
		case newline:
			r.save()
			r.state = eatCRNL
		default:
			r.add(c)
			r.state = inField
		}
	case eatCRNL:
		if !newline {
			r.state = startRecord // CPython stops with an error here; a split line never has it
		}
	}
}

// Feed reads one line; it returns the record when the line ends one.
func (r *Reader) Feed(text string) ([]string, bool) {
	for _, c := range text {
		r.char(c)
	}
	switch r.state {
	case startField, inField, quoteInQuoted:
		r.save()
	case inQuoted:
		return nil, false
	}
	r.state = startRecord
	return r.take(), true
}

// End is what the reader returns when the input ends inside a quoted field: the record so far.
func (r *Reader) End() ([]string, bool) {
	if !r.any && r.state != inQuoted {
		return nil, false
	}
	r.save()
	r.state = startRecord
	return r.take(), true
}

func (r *Reader) take() []string {
	out := r.fields
	if out == nil {
		out = []string{}
	}
	r.fields = nil
	return out
}

// ReadAll is list(csv.reader(lines)) over text split into lines as open(newline="") splits it.
func ReadAll(text string) [][]string {
	var out [][]string
	var r Reader
	for _, l := range SplitLines(text) {
		if row, ok := r.Feed(l); ok {
			out = append(out, row)
		}
	}
	if row, ok := r.End(); ok {
		out = append(out, row)
	}
	return out
}
