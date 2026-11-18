package places

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const (
	defaultLimit  = 5
	maxLimit      = 10
	defaultRadius = 2000
	maxRadius     = 25000
)

func str(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func num(args map[string]any, key string, def, lo, hi int) int {
	n := def
	switch v := args[key].(type) {
	case float64:
		n = int(v)
	case int:
		n = v
	}
	return min(max(n, lo), hi)
}

// SearchTool is places.search.
type SearchTool struct{ Client *Client }

func (t *SearchTool) ID() string          { return "places.search" }
func (t *SearchTool) DisplayName() string { return "Find Places" }
func (t *SearchTool) Description() string { return "Find places, businesses, and addresses" }

// Execute finds a kind of place near somewhere ("coffee" near "Juneau"),
// or places by name or address.
func (t *SearchTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	query, near := str(args, "query"), str(args, "near")
	if query == "" {
		return nil, fmt.Errorf("query required: what or where to find, such as \"coffee\" or \"Juneau public library\"")
	}
	limit := num(args, "limit", defaultLimit, 1, maxLimit)
	tag, isKind := kindOf(query)
	fromHome := false
	if isKind && near == "" {
		// "Coffee near me": where they said they are (#210).
		if near = t.Client.home(ctx); near != "" {
			fromHome = true
		}
	}
	if isKind && near != "" {
		center, err := t.Client.Locate(ctx, near)
		if err != nil {
			return nil, err
		}
		radius := num(args, "radius_m", defaultRadius, 100, maxRadius)
		found, err := t.Client.Nearby(ctx, tag, center.Lat, center.Lon, radius, limit)
		if err != nil {
			// The public Overpass service is often busy; the geocoder knows
			// "cafe in Juneau" too, if less completely.
			out, err := t.byName(ctx, tag.Label+" in "+near, limit, &center)
			if err == nil && fromHome {
				out["near_from"] = homeNote
			}
			return out, err
		}
		// Nothing close: look farther once.
		if len(found) == 0 && radius < maxRadius {
			radius = min(radius*5, maxRadius)
			if found, err = t.Client.Nearby(ctx, tag, center.Lat, center.Lon, radius, limit); err != nil {
				return nil, err
			}
		}
		out := map[string]any{"places": found, "near": center.Address, "kind": tag.Label, "radius_m": radius, "attribution": Attribution}
		if fromHome {
			out["near_from"] = homeNote
		}
		return out, nil
	}
	q := query
	if near != "" {
		q += ", " + near
	}
	return t.byName(ctx, q, limit, nil)
}

// byName searches the geocoder, with distances from center when known.
func (t *SearchTool) byName(ctx context.Context, q string, limit int, center *Place) (map[string]any, error) {
	found, err := t.Client.Search(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"places": found, "attribution": Attribution}
	if center != nil {
		for i := range found {
			found[i].Distance = dist(haversine(center.Lat, center.Lon, found[i].Lat, found[i].Lon))
		}
		sort.SliceStable(found, func(i, j int) bool { return found[i].Distance.Meters < found[j].Distance.Meters })
		out["near"] = center.Address
	}
	return out, nil
}

// DetailsTool is places.details.
type DetailsTool struct{ Client *Client }

func (t *DetailsTool) ID() string          { return "places.details" }
func (t *DetailsTool) DisplayName() string { return "Place Details" }
func (t *DetailsTool) Description() string { return "Look up a place's address, hours, and contacts" }

func (t *DetailsTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	p, err := t.Client.Details(ctx, str(args, "id"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"place": p, "attribution": Attribution}, nil
}

func mode(args map[string]any) string {
	m := strings.ToLower(str(args, "mode"))
	switch m {
	case "", "car", "drive":
		return "driving"
	case "walk", "foot":
		return "walking"
	case "bike", "bicycle":
		return "cycling"
	}
	return m
}

// homeNote says a place came from the person's Personal settings, so the
// answer can say so.
const homeNote = "the user's location in Personal settings"

// endpoints are a route's start and end. fromHome says the start is the
// person's location, as they left from out.
func endpoints(ctx context.Context, c *Client, args map[string]any) (from, to Place, fromHome bool, err error) {
	start := str(args, "from")
	if start == "" {
		// "How far is the airport?": from where they said they are (#210).
		start = c.home(ctx)
		fromHome = start != ""
	}
	if from, err = c.Locate(ctx, start); err != nil {
		return Place{}, Place{}, false, fmt.Errorf("from: %w", err)
	}
	if to, err = c.Locate(ctx, str(args, "to")); err != nil {
		return Place{}, Place{}, false, fmt.Errorf("to: %w", err)
	}
	return from, to, fromHome, nil
}

// maxSteps keeps directions short enough to read.
const maxSteps = 40

// RouteTool is maps.route.
type RouteTool struct{ Client *Client }

func (t *RouteTool) ID() string          { return "maps.route" }
func (t *RouteTool) DisplayName() string { return "Directions" }
func (t *RouteTool) Description() string { return "Directions between two places" }

func (t *RouteTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	from, to, fromHome, err := endpoints(ctx, t.Client, args)
	if err != nil {
		return nil, err
	}
	m := mode(args)
	r, err := t.Client.RouteBetween(ctx, from, to, m, true)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"from": label(from), "to": label(to), "mode": m, "distance": r.Distance, "minutes": r.Minutes, "steps": r.Steps, "attribution": Attribution}
	if fromHome {
		out["from_note"] = homeNote
	}
	if len(r.Steps) > maxSteps {
		out["steps"], out["steps_omitted"] = r.Steps[:maxSteps], len(r.Steps)-maxSteps
	}
	return out, nil
}

// DistanceTool is maps.distance.
type DistanceTool struct{ Client *Client }

func (t *DistanceTool) ID() string          { return "maps.distance" }
func (t *DistanceTool) DisplayName() string { return "Distance" }
func (t *DistanceTool) Description() string {
	return "How far apart two places are, and how long it takes"
}

func (t *DistanceTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	from, to, fromHome, err := endpoints(ctx, t.Client, args)
	if err != nil {
		return nil, err
	}
	m := mode(args)
	out := map[string]any{"from": label(from), "to": label(to), "mode": m,
		"straight_line": dist(haversine(from.Lat, from.Lon, to.Lat, to.Lon)), "attribution": Attribution}
	if fromHome {
		out["from_note"] = homeNote
	}
	r, err := t.Client.RouteBetween(ctx, from, to, m, false)
	if err != nil {
		// Across water, say, there may be no route; the straight line still answers.
		out["route_error"] = err.Error()
		return out, nil
	}
	out["distance"], out["minutes"] = r.Distance, r.Minutes
	return out, nil
}

func label(p Place) string {
	if p.Address != "" {
		return p.Address
	}
	return p.Name
}
