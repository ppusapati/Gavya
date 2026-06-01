package localize

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"

	"p9e.in/samavaya/packages/transport"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"google.golang.org/grpc"
)

type (
	localizerKey struct{}

	FileBundle struct {
		Fs fs.FS
	}
)

var (
	globalFileBundles []FileBundle
	globalLock        sync.RWMutex
)

func RegisterFileBundle(files ...FileBundle) {
	globalLock.Lock()
	defer globalLock.Unlock()
	globalFileBundles = append(globalFileBundles, files...)
}

func I18N(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	globalLock.RLock()
	defer globalLock.RUnlock()
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	bundle.RegisterUnmarshalFunc("yaml", yaml.Unmarshal)
	for _, f := range globalFileBundles {
		fs.WalkDir(f.Fs, ".", func(path string, d fs.DirEntry, err error) error {
			if filepath.Ext(path) == ".toml" || filepath.Ext(path) == ".json" || filepath.Ext(path) == ".yaml" {
				file, err := f.Fs.Open(path)
				if err != nil {
					// Log error and continue instead of panicking
					return nil
				}
				defer file.Close()

				b, err := io.ReadAll(file)
				if err != nil {
					// Log error and continue instead of panicking
					return nil
				}
				bundle.MustParseMessageFileBytes(b, path)
			}
			return nil
		})
	}
	if tr, ok := transport.FromServerContext(ctx); ok {
		accept := tr.RequestHeader().Get("accept-language")
		localizer := i18n.NewLocalizer(bundle, accept)
		ctx = context.WithValue(ctx, localizerKey{}, localizer)
	}
	return handler(ctx, req)
}

// FromContext resolve *i18n.Localizer from context. return nil if not found
func FromContext(ctx context.Context) *i18n.Localizer {
	if ret, ok := ctx.Value(localizerKey{}).(*i18n.Localizer); ok {
		return ret
	}
	return nil
}

func GetMsg(ctx context.Context, id, defaultMsg string, data map[string]interface{}, pluralCount interface{}) string {
	l := FromContext(ctx)
	if l == nil {
		return defaultMsg
	}
	msg, err := l.Localize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID: id,
		},
		TemplateData: data,
		PluralCount:  pluralCount,
	})
	if err != nil {
		return defaultMsg
	}
	return msg
}
