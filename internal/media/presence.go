package media

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// ItemExists checks the original server item ID. An ambiguous response,
// such as an unsupported endpoint or a permission error, remains an error.
func ItemExists(ctx context.Context, client Client, item Item) (bool, error) {
	if client == nil || item.ID == "" {
		return false, ErrResponse
	}
	if flymoo, ok := client.(*flymooClient); ok {
		return flymoo.itemExists(ctx, item.ID)
	}
	found, err := client.Detail(ctx, item)
	switch {
	case errors.Is(err, StatusError(http.StatusNotFound)), errors.Is(err, StatusError(http.StatusGone)):
		return false, nil
	case err != nil:
		return false, err
	case found.ID != item.ID:
		return false, ErrResponse
	default:
		return true, nil
	}
}

func (f *flymooClient) itemExists(ctx context.Context, id string) (bool, error) {
	data, err := f.call(ctx, http.MethodGet, "item/"+url.PathEscape(id), nil, nil, f.session.Token)
	switch {
	case errors.Is(err, flymooError(-6)), errors.Is(err, flymooError(-11)), errors.Is(err, StatusError(http.StatusGone)):
		return false, nil
	case err != nil:
		return false, err
	}
	object := flymooObject(data)
	for _, key := range []string{"item", "info", "detail", "movie", "tv"} {
		if nested, ok := object[key].(map[string]any); ok {
			object = nested
			break
		}
	}
	if flymooText(object, "guid", "id", "item_guid") != id {
		return false, ErrResponse
	}
	return true, nil
}
