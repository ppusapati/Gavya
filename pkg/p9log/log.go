package p9log

import (
	"context"
	"log"
)

// DefaultLogger is default logger.
var DefaultLogger = NewStdLogger(log.Writer())

// Logger is a logger interface.
type Logger interface {
	Log(level Level, keyvals ...interface{}) error
}

type logger struct {
	logger    Logger
	prefix    []interface{}
	hasValuer bool
	ctx       context.Context
}

func (c *logger) Log(level Level, keyvals ...interface{}) error {
	kvs := make([]interface{}, 0, len(c.prefix)+len(keyvals))
	kvs = append(kvs, c.prefix...)
	if c.hasValuer {
		bindValues(c.ctx, kvs)
	}
	kvs = append(kvs, keyvals...)
	if err := c.logger.Log(level, kvs...); err != nil {
		return err
	}
	return nil
}

// With logger fields.
func With(l Logger, kv ...interface{}) Logger {
	c, ok := l.(*logger)
	// fmt.Println("@39")
	// fmt.Fprintln(os.Stdout, []any{kv}...)
	// fmt.Println(containsValuer(kv))
	if !ok {
		return &logger{logger: l, prefix: kv, hasValuer: containsValuer(kv), ctx: context.Background()}
	}
	kvs := make([]interface{}, 0, len(c.prefix)+len(kv))
	kvs = append(kvs, kv...)
	//kvs = append(kvs, c.prefix...)
	return &logger{
		logger:    l,
		prefix:    kvs,
		hasValuer: c.hasValuer, //containsValuer(kvs),
		ctx:       c.ctx,
	}
}

// WithContext returns a shallow copy of l with its context changed
// to ctx. The provided ctx must be non-nil.
func WithContext(ctx context.Context, l Logger) Logger {
	c, ok := l.(*logger)
	if !ok {
		return &logger{logger: l, ctx: ctx}
	}
	return &logger{
		logger:    l,
		prefix:    c.prefix,
		hasValuer: c.hasValuer,
		ctx:       ctx,
	}
}
