// Package p9log is the structured logging facade used throughout the
// platform. It wraps a Zap core behind a minimal Logger interface:
//
//	type Logger interface {
//	    Log(level Level, keyvals ...interface{}) error
//	}
//
// The Logger interface is DELIBERATELY minimal. The ergonomic level
// methods (Debug / Info / Warn / Error, plus `-f` / `-w` variants) live
// on *Helper, which wraps a Logger:
//
//	helper := p9log.NewHelper(logger)
//	helper.Infof("user %s logged in", userID)
//	helper.Errorw("save failed", "err", err, "user_id", userID)
//
// Struct fields that want the level methods must be typed as *p9log.Helper,
// not p9log.Logger. Constructors typically accept Logger (for caller
// flexibility) and wrap via NewHelper internally — see the B.1 sweep
// (roadmap 2026-04-19) for the 10+ packages corrected after this drift.
//
// Context propagation:
//
//	p9log.Context(ctx)              // *Helper already tied to ctx fields
//	p9log.WithContext(helper, ctx)  // copy helper with context fields added
//
// File sinks:
//
//	w := p9log.LoggerFile(path, threshold)  // size-limited rotating writer
//
// Global default:
//
//	p9log.Default()         // process-wide default Logger
//	p9log.SetDefault(log)   // replace (bootstrap path only)
package p9log
