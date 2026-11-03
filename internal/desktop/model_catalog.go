package desktop

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	modelCatalogTTL = time.Minute
	// Session builds may fall back to an expired snapshot of the same account
	// while the Bridge is unreachable; beyond this age it is no longer trusted.
	modelCatalogStaleLimit = 24 * time.Hour
	// After a failed fetch, session builds stop asking the Bridge for this long
	// and go straight to the fallback instead of waiting out another timeout.
	modelCatalogRetryAfter = 30 * time.Second
)

// errModelCatalogUnavailable marks a failed catalog fetch, as opposed to a
// catalog that was fetched and answered (e.g. "model not authorized").
var errModelCatalogUnavailable = errors.New("获取平台模型目录失败")

type modelCatalog struct {
	models map[string]PassportModel
	// visionDefaults comes from the raw Bridge list, before ID dedupe: two
	// entries claiming vision_default are a misconfiguration to report, not
	// something the map may quietly collapse into one.
	visionDefaults []PassportModel
	at             time.Time
}

type modelCatalogFailure struct {
	err error
	at  time.Time
}

func modelCatalogKey(scope modelReference, actor string) string {
	scope.Name = ""
	return fmt.Sprintf("%q/%q/%q", scope.Bridge, scope.TenantID, actor)
}

// Catalogs are immutable snapshots. Publish under the requesting identity, never
// whichever account happens to be logged in when a slow response arrives.
func (a *App) rememberModelCatalog(scope modelReference, actor string, models []PassportModel) {
	next := modelCatalog{models: make(map[string]PassportModel, len(models)), at: time.Now()}
	for _, m := range models {
		next.models[m.ID] = m
		if m.VisionDefault {
			next.visionDefaults = append(next.visionDefaults, m)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.modelCatalogs == nil || len(a.modelCatalogs) >= 64 {
		a.modelCatalogs = map[string]modelCatalog{}
	}
	key := modelCatalogKey(scope, actor)
	a.modelCatalogs[key] = next
	delete(a.modelCatalogFailures, key)
}

func (a *App) platformModelCatalog(ctx context.Context, scope modelReference, actor string) (modelCatalog, error) {
	if err := ctx.Err(); err != nil {
		return modelCatalog{}, err
	}
	if !a.tokens.LoggedIn() {
		return modelCatalog{}, errors.New("加载平台模型目录需要登录通行证")
	}
	if a.visionAccountIdentity() != actor {
		return modelCatalog{}, errors.New("加载模型目录期间账号已变化，请重试")
	}
	if scope != platformModelRef(scope.TenantID, "") {
		return modelCatalog{}, errors.New("会话所属平台已变化，请重新打开会话")
	}
	key := modelCatalogKey(scope, actor)
	a.mu.Lock()
	entry, ok := a.modelCatalogs[key]
	a.mu.Unlock()
	if ok && time.Since(entry.at) < modelCatalogTTL {
		return entry, nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if _, err := a.passportModelsContext(lookupCtx, scope.TenantID); err != nil {
		err = fmt.Errorf("%w: %w", errModelCatalogUnavailable, err)
		// A caller that gave up is not evidence that the Bridge is down.
		if ctx.Err() == nil {
			a.mu.Lock()
			if a.modelCatalogFailures == nil || len(a.modelCatalogFailures) >= 64 {
				a.modelCatalogFailures = map[string]modelCatalogFailure{}
			}
			a.modelCatalogFailures[key] = modelCatalogFailure{err: err, at: time.Now()}
			a.mu.Unlock()
		}
		return modelCatalog{}, err
	}
	if err := lookupCtx.Err(); err != nil {
		return modelCatalog{}, err
	}
	if a.visionAccountIdentity() != actor {
		return modelCatalog{}, errors.New("加载模型目录期间账号已变化，请重试")
	}
	a.mu.Lock()
	entry, ok = a.modelCatalogs[key]
	a.mu.Unlock()
	if !ok {
		return modelCatalog{}, errors.New("平台模型目录已变化，请重试")
	}
	return entry, nil
}

// staleModelCatalog returns the last snapshot fetched for this exact scope and
// account, even past its TTL. Only session builds use it: settings and vision
// keep failing explicitly when a refresh fails.
func (a *App) staleModelCatalog(scope modelReference, actor string) (modelCatalog, bool) {
	if a.visionAccountIdentity() != actor || scope != platformModelRef(scope.TenantID, "") {
		return modelCatalog{}, false
	}
	a.mu.Lock()
	entry, ok := a.modelCatalogs[modelCatalogKey(scope, actor)]
	a.mu.Unlock()
	if !ok || time.Since(entry.at) >= modelCatalogStaleLimit {
		return modelCatalog{}, false
	}
	return entry, true
}

// buildModelCatalog is the catalog lookup for session builds. A fetch that just
// failed is not retried for modelCatalogRetryAfter: the build answers from this
// account's stale snapshot, or reports the outage, without another timeout.
// Settings and vision keep using platformModelCatalog, so a manual refresh
// there always reaches the Bridge.
func (a *App) buildModelCatalog(ctx context.Context, scope modelReference, actor string) (modelCatalog, error) {
	a.mu.Lock()
	failure, failed := a.modelCatalogFailures[modelCatalogKey(scope, actor)]
	a.mu.Unlock()
	var err error
	if failed && time.Since(failure.at) < modelCatalogRetryAfter {
		err = failure.err
	} else {
		var catalog modelCatalog
		if catalog, err = a.platformModelCatalog(ctx, scope, actor); !errors.Is(err, errModelCatalogUnavailable) {
			return catalog, err
		}
	}
	if stale, ok := a.staleModelCatalog(scope, actor); ok {
		return stale, nil
	}
	return modelCatalog{}, err
}
