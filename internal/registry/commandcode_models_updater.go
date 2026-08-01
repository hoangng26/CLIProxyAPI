package registry

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const maxCommandCodeModelsSize = 4 << 20

var commandCodeModelsUpdaterOnce sync.Once

// StartCommandCodeModelsUpdater starts a background updater that fetches the
// CommandCode model catalog immediately and then refreshes it every 3 hours.
// Safe to call multiple times; only one updater will run.
func StartCommandCodeModelsUpdater(ctx context.Context) {
	commandCodeModelsUpdaterOnce.Do(func() {
		go runCommandCodeModelsUpdater(ctx)
	})
}

func runCommandCodeModelsUpdater(ctx context.Context) {
	tryRefreshCommandCodeModels(ctx, "startup CommandCode model refresh")

	ticker := time.NewTicker(modelsRefreshInterval)
	defer ticker.Stop()
	log.Infof("periodic CommandCode model refresh started (interval=%s)", modelsRefreshInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tryRefreshCommandCodeModels(ctx, "periodic CommandCode model refresh")
		}
	}
}

func tryRefreshCommandCodeModels(ctx context.Context, label string) {
	data, sourceURL := fetchCommandCodeModelsFromRemote(ctx)
	if data == nil {
		log.Warnf("%s: fetch failed, keeping current data", label)
		return
	}

	models, errParse := parseCommandCodeRemoteModels(data)
	if errParse != nil {
		log.Warnf("%s: fetched catalog rejected, keeping current data: %v", label, errParse)
		return
	}

	commandCodeCatalogStore.mu.RLock()
	old := cloneModelInfos(commandCodeCatalogStore.data)
	commandCodeCatalogStore.mu.RUnlock()

	if commandCodeModelsEqual(old, models) {
		log.Infof("%s completed from %s, no changes detected", label, sourceURL)
		return
	}

	if errLoad := loadCommandCodeModels(models, sourceURL); errLoad != nil {
		log.Warnf("%s: failed to apply catalog: %v", label, errLoad)
		return
	}
	log.Infof("%s completed from %s, catalog updated (%d models)", label, sourceURL, len(models))
	notifyModelRefresh([]string{"commandcode"})
}

func fetchCommandCodeModelsFromRemote(ctx context.Context) ([]byte, string) {
	client := &http.Client{Timeout: modelsFetchTimeout}
	sourceURL := commandCodeModelsURL
	reqCtx, cancel := context.WithTimeout(ctx, modelsFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, sourceURL, nil)
	if err != nil {
		log.Debugf("CommandCode models fetch request creation failed for %s: %v", sourceURL, err)
		return nil, ""
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Debugf("CommandCode models fetch failed from %s: %v", sourceURL, err)
		return nil, ""
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Debugf("CommandCode models response close failed for %s: %v", sourceURL, errClose)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		log.Debugf("CommandCode models fetch returned %d from %s", resp.StatusCode, sourceURL)
		return nil, ""
	}

	data, errRead := io.ReadAll(io.LimitReader(resp.Body, maxCommandCodeModelsSize+1))
	if errRead != nil {
		log.Debugf("CommandCode models fetch read error from %s: %v", sourceURL, errRead)
		return nil, ""
	}
	if len(data) > maxCommandCodeModelsSize {
		log.Warnf("CommandCode models fetch from %s exceeded %d bytes", sourceURL, maxCommandCodeModelsSize)
		return nil, ""
	}
	return data, sourceURL
}
