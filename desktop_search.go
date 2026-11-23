package main

import (
	"context"
	"errors"
	"sync"
	"time"
)

func (a *application) loadDesktopSearch(ctx context.Context, request catalogRequest) catalogResult {
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	var credits catalogResult
	var workers sync.WaitGroup
	workers.Go(func() { credits = a.loadSearchCredits(ctx, request) })
	titles := request
	titles.merge = false
	result := a.loadCatalog(ctx, titles)
	workers.Wait()

	// Merge the two result sets before applying the desktop page size so a
	// credit match for a title already shown never crowds out another result.
	result.items = combineSearchCatalog(&request, result.items, credits.items, nil)
	result.hasMore = result.hasMore || credits.hasMore || len(result.items) > request.limit
	result.items = result.items[:min(len(result.items), request.limit)]
	result.connected = result.connected || credits.connected
	result.failure = errors.Join(result.failure, credits.failure)
	return result
}
