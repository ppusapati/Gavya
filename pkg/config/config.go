package config

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"

	"google.golang.org/grpc/keepalive"

	// init encoding
	"p9e.in/samavaya/packages/database/pgxpostgres"
	_ "p9e.in/samavaya/packages/encoding/json"
	_ "p9e.in/samavaya/packages/encoding/proto"
	_ "p9e.in/samavaya/packages/encoding/xml"
	_ "p9e.in/samavaya/packages/encoding/yaml"
	"p9e.in/samavaya/packages/p9log"
)

var _ Config = (*config)(nil)

var (
	// ErrNotFound is key not found.
	ErrNotFound = errors.New("key not found")
	// ErrTypeAssert is type assert error.
	ErrTypeAssert = errors.New("type assert error")
)

// Observer is config observer.
type Observer func(string, Value)

// Config is a config interface.
type Config interface {
	Load() error
	Scan(v interface{}) error
	Value(key string) Value
	Watch(key string, o Observer) error
	Close() error
}

type config struct {
	opts      options
	reader    Reader
	cached    sync.Map
	observers sync.Map
}

type GrpcServerConfig struct {
	Port            uint32
	KeepaliveParams keepalive.ServerParameters
	KeepalivePolicy keepalive.EnforcementPolicy
	DBContext       *pgxpostgres.DBContext
}

// New a config with options.
func New(opts ...Option) Config {
	o := options{
		decoder:  defaultDecoder,
		resolver: defaultResolver,
	}
	for _, opt := range opts {
		opt(&o)
	}
	return &config{
		opts:   o,
		reader: newReader(o),
	}
}

func (c *config) watch(w Watcher) {
	for {
		kvs, err := w.Next()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				p9log.Infof("watcher's ctx cancel : %v", err)
				return
			}
			time.Sleep(time.Second)
			p9log.Errorf("failed to watch next config: %v", err)
			continue
		}
		if err := c.reader.Merge(kvs...); err != nil {
			p9log.Errorf("failed to merge next config: %v", err)
			continue
		}
		if err := c.reader.Resolve(); err != nil {
			p9log.Errorf("failed to resolve next config: %v", err)
			continue
		}
		c.cached.Range(func(key, value interface{}) bool {
			k := key.(string)
			v := value.(Value)
			if n, ok := c.reader.Value(k); ok && reflect.TypeOf(n.Load()) == reflect.TypeOf(v.Load()) && !reflect.DeepEqual(n.Load(), v.Load()) {
				v.Store(n.Load())
				if o, ok := c.observers.Load(k); ok {
					o.(Observer)(k, v)
				}
			}
			return true
		})
	}
}

func (c *config) Load() error {
	for _, src := range c.opts.sources {
		kvs, err := src.Load()
		if err != nil {
			return err
		}
		for _, v := range kvs {
			p9log.Debugf("config loaded: %s format: %s", v.Key, v.Format)
		}
		if err = c.reader.Merge(kvs...); err != nil {
			p9log.Errorf("failed to merge config source: %v", err)
			return err
		}
		// NOTE: Config watching is not implemented yet.
		// If needed in the future, uncomment and implement:
		// w, err := src.Watch()
		// if err != nil { return err }
		// go c.watch(w)
	}
	if err := c.reader.Resolve(); err != nil {
		p9log.Errorf("failed to resolve config source: %v", err)
		return err
	}
	return nil
}

func (c *config) Value(key string) Value {
	if v, ok := c.cached.Load(key); ok {
		return v.(Value)
	}
	if v, ok := c.reader.Value(key); ok {
		c.cached.Store(key, v)
		return v
	}
	return &errValue{err: ErrNotFound}
}

func (c *config) Scan(v interface{}) error {
	data, err := c.reader.Source()
	if err != nil {
		return err
	}
	return unmarshalJSON(data, v)
}

func (c *config) Watch(key string, o Observer) error {
	if v := c.Value(key); v.Load() == nil {
		return ErrNotFound
	}
	c.observers.Store(key, o)
	return nil
}

func (c *config) Close() error {
	// NOTE: No watchers to close yet. If watching is implemented,
	// add cleanup logic here.
	return nil
}
