package projects

import "context"

type idKey struct{}

// WithID asks for a chat's turn in a project: a new chat goes into it
// (#1136). The caller checks the project is the person's.
func WithID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, idKey{}, id)
}

// IDFrom is the project a turn was asked for in, or "".
func IDFrom(ctx context.Context) string {
	id, _ := ctx.Value(idKey{}).(string)
	return id
}
