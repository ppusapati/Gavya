package p9log

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"sync"

	"github.com/fatih/color"
)

var (
	debugColor = color.New(color.FgBlue)
	infoColor  = color.New(color.FgGreen)
	warnColor  = color.New(color.FgYellow)
	errorColor = color.New(color.FgRed)
	fatalColor = color.New(color.BgRed, color.FgWhite)
)

var _ Logger = (*stdLogger)(nil)

type stdLogger struct {
	log  *log.Logger
	pool *sync.Pool

	debugColor *color.Color
	infoColor  *color.Color
	warnColor  *color.Color
	errorColor *color.Color
	fatalColor *color.Color
}

// NewStdLogger new a logger with writer.
func NewStdLogger(w io.Writer) Logger {

	return &stdLogger{
		log: log.New(w, "", 0),
		pool: &sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
		debugColor: debugColor,
		infoColor:  infoColor,
		warnColor:  warnColor,
		errorColor: errorColor,
		fatalColor: fatalColor,
	}
}

// Log print the kv pairs log.
func (l *stdLogger) Log(level Level, keyvals ...interface{}) error {
	if len(keyvals) == 0 {
		return nil
	}
	if (len(keyvals) & 1) == 1 {
		keyvals = append(keyvals, "KEYVALS UNPAIRED")
	}
	buf := l.pool.Get().(*bytes.Buffer)
	switch level {
	case LevelDebug:
		l.debugColor.Fprint(buf, level.String())
	case LevelInfo:
		l.infoColor.Fprint(buf, level.String())
	case LevelWarn:
		l.warnColor.Fprint(buf, level.String())
	case LevelError:
		l.errorColor.Fprint(buf, level.String())
	case LevelFatal:
		l.fatalColor.Fprint(buf, level.String())
	}
	for i := 0; i < len(keyvals); i += 2 {
		_, _ = fmt.Fprintf(buf, "\n %s=%v", keyvals[i], keyvals[i+1])
	}
	_ = l.log.Output(4, buf.String()) //nolint:gomnd
	buf.Reset()
	l.pool.Put(buf)
	return nil
}
