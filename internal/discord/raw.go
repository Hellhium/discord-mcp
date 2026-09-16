package discord

import (
	"context"
	"net/url"
)

var rawMethods = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}

// DoRaw sends a discord_request call. The route is validated by
// ValidateRawRoute and always appended to APIBase; the recorder stores the
// redacted route.
func (c *Client) DoRaw(ctx context.Context, method, route string, query url.Values, body any, reason string) (Response, error) {
	if !rawMethods[method] {
		return Response{}, &ArgError{Msg: "method must be one of GET, POST, PUT, PATCH, DELETE"}
	}
	if err := ValidateRawRoute(route); err != nil {
		return Response{}, err
	}
	call := Call{Method: method, Route: route, Query: query, Body: body, Reason: reason}
	return c.send(ctx, call, route, RedactRoute(route), method+" "+route)
}
